package worker

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	pb "dist-command-exec/gen/go/distexec/v1"
	"dist-command-exec/internal/config"
	"dist-command-exec/pkg/executor"
)

// Server is the worker gRPC server.
type Server struct {
	cfg          config.Worker
	grpcServer   *grpc.Server
	taskRunner   *TaskRunner
	registration *Registration
	healthSrv    *health.Server

	pb.UnimplementedWorkerServiceServer
}

// NewServer creates a new worker server.
func NewServer(cfg config.Worker, registry *executor.Registry) (*Server, error) {
	taskRunner := NewTaskRunner(registry, cfg.MaxConcurrentTasks)

	registration := NewRegistration(RegistrationConfig{
		WorkerID:     cfg.ID,
		Address:      cfg.Address,
		MasterAddr:   cfg.MasterAddress,
		Capabilities: registry.Capabilities(),
		MaxTasks:     cfg.MaxConcurrentTasks,
		Labels:       nil,
	})

	s := &Server{
		cfg:          cfg,
		taskRunner:   taskRunner,
		registration: registration,
	}

	// Setup gRPC server
	s.grpcServer = grpc.NewServer()
	pb.RegisterWorkerServiceServer(s.grpcServer, s)

	// Health service
	s.healthSrv = health.NewServer()
	grpc_health_v1.RegisterHealthServer(s.grpcServer, s.healthSrv)
	s.healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	return s, nil
}

// Run starts the worker server.
func (s *Server) Run(ctx context.Context) error {
	lis, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	log.Info().
		Str("address", s.cfg.Address).
		Str("worker_id", s.cfg.ID).
		Msg("worker server starting")

	// Start gRPC server in background
	serverReady := make(chan struct{})
	go func() {
		close(serverReady)
		if err := s.grpcServer.Serve(lis); err != nil {
			log.Error().Err(err).Msg("gRPC server error")
		}
	}()

	// Wait for server to be ready
	<-serverReady
	time.Sleep(100 * time.Millisecond) // Brief delay for listener to be active

	// Register with master
	if err := s.registration.Register(ctx); err != nil {
		log.Warn().Err(err).Msg("initial registration failed, will retry via heartbeat")
	}

	// Start heartbeat loop
	go s.registration.HeartbeatLoop(ctx, s.taskRunner.ActiveCount)

	// Handle shutdown
	<-ctx.Done()
	return s.Shutdown(context.Background())
}

// Shutdown gracefully shuts down the worker.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Info().Str("worker_id", s.cfg.ID).Msg("shutdown initiated")

	// Stop accepting new tasks
	s.healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	// Deregister from master
	if err := s.registration.Deregister(ctx); err != nil {
		log.Warn().Err(err).Msg("deregistration failed")
	}

	// Wait for in-flight tasks
	log.Info().Int("active_tasks", s.taskRunner.ActiveCount()).Msg("draining tasks")
	drainCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s.taskRunner.WaitForDrain(drainCtx)

	// Stop gRPC server
	s.grpcServer.GracefulStop()

	// Close master connection
	s.registration.Close()

	log.Info().Str("worker_id", s.cfg.ID).Msg("shutdown complete")
	return nil
}

// --- WorkerService Implementation ---

// Execute handles task execution.
func (s *Server) Execute(ctx context.Context, req *pb.ExecuteRequest) (*pb.ExecuteResponse, error) {
	if req.TaskId == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	if req.TaskType == "" {
		return nil, status.Error(codes.InvalidArgument, "task_type is required")
	}

	// Determine timeout
	timeout := 30 * time.Second
	if req.Timeout != nil {
		timeout = req.Timeout.AsDuration()
	}

	// Build executor task
	task := executor.Task{
		ID:       req.TaskId,
		Type:     req.TaskType,
		Payload:  req.Payload,
		Metadata: req.Metadata,
	}

	// Execute
	start := time.Now()
	result, err := s.taskRunner.Run(ctx, task, timeout)
	duration := time.Since(start)

	if err != nil {
		if err == ErrAtCapacity {
			return nil, status.Error(codes.ResourceExhausted, "worker at capacity")
		}
		return nil, status.Errorf(codes.Internal, "execution error: %v", err)
	}

	// Build response
	resp := &pb.ExecuteResponse{
		TaskId:   req.TaskId,
		Output:   result.Output,
		Error:    result.Error,
		ExitCode: int32(result.ExitCode),
		Duration: durationpb.New(duration),
	}

	if result.Error != "" || result.ExitCode != 0 {
		resp.Status = pb.ExecutionStatus_EXECUTION_STATUS_FAILURE
	} else {
		resp.Status = pb.ExecutionStatus_EXECUTION_STATUS_SUCCESS
	}

	return resp, nil
}

// ExecuteStream handles streaming task execution.
func (s *Server) ExecuteStream(req *pb.ExecuteRequest, stream pb.WorkerService_ExecuteStreamServer) error {
	// TODO: Implement streaming execution
	return status.Error(codes.Unimplemented, "streaming execution not yet implemented")
}

// CancelTask cancels a running task.
func (s *Server) CancelTask(ctx context.Context, req *pb.CancelTaskRequest) (*pb.CancelTaskResponse, error) {
	if req.TaskId == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}

	cancelled := s.taskRunner.CancelTask(req.TaskId)
	return &pb.CancelTaskResponse{Cancelled: cancelled}, nil
}

// GetWorkerStatus returns worker status.
func (s *Server) GetWorkerStatus(ctx context.Context, req *pb.GetWorkerStatusRequest) (*pb.GetWorkerStatusResponse, error) {
	// Run health checks
	healthResults := s.taskRunner.HealthCheck(ctx)
	health := pb.WorkerHealth_WORKER_HEALTH_HEALTHY
	for _, err := range healthResults {
		if err != nil {
			health = pb.WorkerHealth_WORKER_HEALTH_DEGRADED
			break
		}
	}

	return &pb.GetWorkerStatusResponse{
		WorkerId:           s.cfg.ID,
		Capabilities:       s.taskRunner.Capabilities(),
		MaxConcurrentTasks: int32(s.taskRunner.MaxTasks()),
		ActiveTasks:        int32(s.taskRunner.ActiveCount()),
		Health:             health,
	}, nil
}
