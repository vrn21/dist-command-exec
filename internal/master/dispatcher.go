package master

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/durationpb"

	pb "dist-command-exec/gen/go/distexec/v1"
	"dist-command-exec/pkg/types"
)

// Dispatcher sends tasks to workers and collects results.
type Dispatcher struct {
	registry  *NodeRegistry
	scheduler *Scheduler
	balancer  LoadBalancer
	scaler    *AutoScaler

	// gRPC client pool
	mu      sync.RWMutex
	clients map[string]pb.WorkerServiceClient
	conns   map[string]*grpc.ClientConn

	// Configuration
	taskTimeout time.Duration
	retryDelay  time.Duration
	maxRetries  int
}

// DispatcherConfig holds dispatcher configuration.
type DispatcherConfig struct {
	TaskTimeout time.Duration
	RetryDelay  time.Duration
	MaxRetries  int
}

// NewDispatcher creates a new task dispatcher.
func NewDispatcher(registry *NodeRegistry, scheduler *Scheduler, balancer LoadBalancer, scaler *AutoScaler, cfg DispatcherConfig) *Dispatcher {
	if cfg.TaskTimeout <= 0 {
		cfg.TaskTimeout = 30 * time.Second
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 1 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}

	return &Dispatcher{
		registry:    registry,
		scheduler:   scheduler,
		balancer:    balancer,
		scaler:      scaler,
		clients:     make(map[string]pb.WorkerServiceClient),
		conns:       make(map[string]*grpc.ClientConn),
		taskTimeout: cfg.TaskTimeout,
		retryDelay:  cfg.RetryDelay,
		maxRetries:  cfg.MaxRetries,
	}
}

// Run starts the dispatch loop.
func (d *Dispatcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.closeConnections()
			return
		default:
			task, err := d.scheduler.Dequeue(ctx)
			if err != nil {
				continue
			}
			go d.dispatchTask(ctx, task)
		}
	}
}

func (d *Dispatcher) dispatchTask(ctx context.Context, task *types.Task) {
	// Ensure capacity before dispatch
	if d.scaler != nil {
		pendingCount := d.scheduler.PendingCount()
		if err := d.scaler.EnsureCapacity(ctx, pendingCount); err != nil {
			log.Warn().Err(err).Msg("scale up failed")
		}
	}

	// Get healthy nodes
	nodes := d.registry.GetHealthyNodes()
	if len(nodes) == 0 {
		log.Warn().Str("task_id", task.ID).Msg("no healthy nodes, requeueing task")
		time.Sleep(d.retryDelay)
		if err := d.scheduler.Requeue(task); err != nil {
			log.Error().Err(err).Str("task_id", task.ID).Msg("failed to requeue task")
		}
		return
	}

	// Select node
	node, err := d.balancer.Select(task, nodes)
	if err != nil {
		log.Error().Err(err).Str("task_id", task.ID).Msg("load balancer failed")
		if err := d.scheduler.Requeue(task); err != nil {
			log.Error().Err(err).Str("task_id", task.ID).Msg("failed to requeue task")
		}
		return
	}

	// Dispatch to worker
	result, err := d.sendToWorker(ctx, node, task)
	if err != nil {
		log.Error().Err(err).
			Str("task_id", task.ID).
			Str("worker_id", node.ID).
			Msg("dispatch failed")

		d.scheduler.MarkFailed(task.ID, err)

		// Retry if possible
		if task.CanRetry() {
			log.Info().Str("task_id", task.ID).Int("retries", task.Retries).Msg("retrying task")
			if err := d.scheduler.Requeue(task); err != nil {
				log.Error().Err(err).Str("task_id", task.ID).Msg("failed to requeue task")
			}
		}
		return
	}

	// Mark complete
	d.scheduler.MarkComplete(task.ID, result)
	d.registry.DecrementActiveTasks(node.ID)

	log.Info().
		Str("task_id", task.ID).
		Str("worker_id", node.ID).
		Dur("duration", result.Duration).
		Msg("task completed")
}

func (d *Dispatcher) sendToWorker(ctx context.Context, node NodeInfo, task *types.Task) (*types.Result, error) {
	client, err := d.getClient(node)
	if err != nil {
		return nil, fmt.Errorf("get client: %w", err)
	}

	// Mark as running
	d.scheduler.MarkRunning(task.ID, node.ID)
	d.registry.IncrementActiveTasks(node.ID)

	// Build request
	req := &pb.ExecuteRequest{
		TaskId:   task.ID,
		TaskType: task.Type,
		Payload:  task.Payload,
		Timeout:  durationpb.New(task.Timeout),
		Metadata: task.Metadata,
	}

	// Execute with timeout
	execCtx, cancel := context.WithTimeout(ctx, task.Timeout)
	defer cancel()

	resp, err := client.Execute(execCtx, req)
	if err != nil {
		d.registry.DecrementActiveTasks(node.ID)
		return nil, fmt.Errorf("execute: %w", err)
	}

	// Convert response to result
	result := &types.Result{
		TaskID:      task.ID,
		Output:      resp.Output,
		ExitCode:    int(resp.ExitCode),
		Error:       resp.Error,
		WorkerID:    node.ID,
		Duration:    resp.Duration.AsDuration(),
		CompletedAt: time.Now(),
		Metadata:    make(map[string]string),
	}

	switch resp.Status {
	case pb.ExecutionStatus_EXECUTION_STATUS_SUCCESS:
		result.Status = types.ResultStatusSuccess
	case pb.ExecutionStatus_EXECUTION_STATUS_TIMEOUT:
		result.Status = types.ResultStatusTimeout
	case pb.ExecutionStatus_EXECUTION_STATUS_CANCELLED:
		result.Status = types.ResultStatusCancelled
	default:
		result.Status = types.ResultStatusFailure
	}

	return result, nil
}

func (d *Dispatcher) getClient(node NodeInfo) (pb.WorkerServiceClient, error) {
	d.mu.RLock()
	client, ok := d.clients[node.ID]
	d.mu.RUnlock()

	if ok {
		return client, nil
	}

	// Create new connection
	d.mu.Lock()
	defer d.mu.Unlock()

	// Double-check
	if client, ok := d.clients[node.ID]; ok {
		return client, nil
	}

	conn, err := grpc.NewClient(node.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", node.Address, err)
	}

	client = pb.NewWorkerServiceClient(conn)
	d.clients[node.ID] = client
	d.conns[node.ID] = conn

	return client, nil
}

func (d *Dispatcher) closeConnections() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for id, conn := range d.conns {
		conn.Close()
		delete(d.clients, id)
		delete(d.conns, id)
	}
}

// RemoveClient removes a client connection (e.g., when node dies).
func (d *Dispatcher) RemoveClient(nodeID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if conn, ok := d.conns[nodeID]; ok {
		conn.Close()
		delete(d.clients, nodeID)
		delete(d.conns, nodeID)
	}
}
