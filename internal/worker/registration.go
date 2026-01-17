package worker

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "dist-command-exec/gen/go/distexec/v1"
)

// Common errors.
var (
	ErrNotRegistered = errors.New("worker not registered")
	ErrAtCapacity    = errors.New("worker at capacity")
)

// Registration handles master registration and heartbeat.
type Registration struct {
	workerID     string
	address      string
	masterAddr   string
	capabilities []string
	maxTasks     int
	labels       map[string]string

	conn   *grpc.ClientConn
	client pb.RegistrationServiceClient

	heartbeatInterval time.Duration
	registered        bool
}

// RegistrationConfig holds registration configuration.
type RegistrationConfig struct {
	WorkerID     string
	Address      string
	MasterAddr   string
	Capabilities []string
	MaxTasks     int
	Labels       map[string]string
}

// NewRegistration creates a new registration handler.
func NewRegistration(cfg RegistrationConfig) *Registration {
	return &Registration{
		workerID:          cfg.WorkerID,
		address:           cfg.Address,
		masterAddr:        cfg.MasterAddr,
		capabilities:      cfg.Capabilities,
		maxTasks:          cfg.MaxTasks,
		labels:            cfg.Labels,
		heartbeatInterval: 5 * time.Second,
	}
}

// Connect establishes connection to master.
func (r *Registration) Connect(ctx context.Context) error {
	conn, err := grpc.NewClient(r.masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	r.conn = conn
	r.client = pb.NewRegistrationServiceClient(conn)
	return nil
}

// Register registers the worker with the master.
func (r *Registration) Register(ctx context.Context) error {
	if r.client == nil {
		if err := r.Connect(ctx); err != nil {
			return err
		}
	}

	resp, err := r.client.Register(ctx, &pb.RegisterRequest{
		WorkerId:           r.workerID,
		Address:            r.address,
		Capabilities:       r.capabilities,
		MaxConcurrentTasks: int32(r.maxTasks),
		Labels:             r.labels,
	})
	if err != nil {
		return err
	}

	if !resp.Accepted {
		return errors.New(resp.Message)
	}

	if resp.HeartbeatInterval != nil {
		r.heartbeatInterval = resp.HeartbeatInterval.AsDuration()
	}

	r.registered = true
	log.Info().
		Str("worker_id", r.workerID).
		Str("master", r.masterAddr).
		Dur("heartbeat_interval", r.heartbeatInterval).
		Msg("registered with master")

	return nil
}

// Deregister deregisters the worker from the master.
func (r *Registration) Deregister(ctx context.Context) error {
	if r.client == nil || !r.registered {
		return nil
	}

	_, err := r.client.Deregister(ctx, &pb.DeregisterRequest{
		WorkerId: r.workerID,
	})
	if err != nil {
		log.Warn().Err(err).Msg("deregistration failed")
		return err
	}

	r.registered = false
	log.Info().Str("worker_id", r.workerID).Msg("deregistered from master")
	return nil
}

// HeartbeatLoop runs the heartbeat loop.
func (r *Registration) HeartbeatLoop(ctx context.Context, getActiveTasks func() int) {
	ticker := time.NewTicker(r.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !r.registered {
				continue
			}

			activeTasks := getActiveTasks()
			resp, err := r.client.Heartbeat(ctx, &pb.HeartbeatRequest{
				WorkerId:    r.workerID,
				ActiveTasks: int32(activeTasks),
				Health:      pb.WorkerHealth_WORKER_HEALTH_HEALTHY,
			})
			if err != nil {
				log.Warn().Err(err).Msg("heartbeat failed")
				continue
			}

			if !resp.Acknowledged {
				log.Warn().Msg("heartbeat not acknowledged, re-registering")
				if err := r.Register(ctx); err != nil {
					log.Error().Err(err).Msg("re-registration failed")
				}
				continue
			}

			// Handle actions from master
			switch resp.Action {
			case pb.HeartbeatAction_HEARTBEAT_ACTION_DRAIN:
				log.Info().Msg("master requested drain")
				// TODO: Set worker to draining mode
			case pb.HeartbeatAction_HEARTBEAT_ACTION_SHUTDOWN:
				log.Info().Msg("master requested shutdown")
				// TODO: Initiate graceful shutdown
			}
		}
	}
}

// Close closes the connection to master.
func (r *Registration) Close() error {
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}

// IsRegistered returns whether the worker is registered.
func (r *Registration) IsRegistered() bool {
	return r.registered
}
