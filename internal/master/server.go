package master

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "dist-command-exec/gen/go/distexec/v1"
	"dist-command-exec/internal/config"
	"dist-command-exec/pkg/types"
)

// Server is the master gRPC server.
type Server struct {
	cfg        config.Master
	grpcServer *grpc.Server
	scheduler  *Scheduler
	registry   *NodeRegistry
	balancer   LoadBalancer
	dispatcher *Dispatcher
	scaler     *AutoScaler
	store      *Store
	healthSrv  *health.Server

	pb.UnimplementedMasterServiceServer
	pb.UnimplementedRegistrationServiceServer
}

// NewServer creates a new master server.
func NewServer(cfg config.Master) (*Server, error) {
	registry := NewNodeRegistry(
		time.Duration(cfg.HeartbeatTimeout)*time.Second,
		time.Duration(cfg.HeartbeatTimeout)*2*time.Second,
	)

	scheduler := NewScheduler(cfg.MaxConcurrentJobs)
	balancer := NewRoundRobinBalancer()
	store := NewStore()
	scaler := NewAutoScaler(registry, DefaultScalerConfig())

	dispatcher := NewDispatcher(registry, scheduler, balancer, scaler, DispatcherConfig{
		TaskTimeout: 30 * time.Second,
		RetryDelay:  1 * time.Second,
		MaxRetries:  3,
	})

	// When a node is removed, clean up its connection
	registry.SetNodeRemovedCallback(func(nodeID string) {
		dispatcher.RemoveClient(nodeID)
	})

	s := &Server{
		cfg:        cfg,
		scheduler:  scheduler,
		registry:   registry,
		balancer:   balancer,
		dispatcher: dispatcher,
		scaler:     scaler,
		store:      store,
	}

	// Setup gRPC server
	s.grpcServer = grpc.NewServer()
	pb.RegisterMasterServiceServer(s.grpcServer, s)
	pb.RegisterRegistrationServiceServer(s.grpcServer, s)

	// Health service
	s.healthSrv = health.NewServer()
	grpc_health_v1.RegisterHealthServer(s.grpcServer, s.healthSrv)
	s.healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	return s, nil
}

// Run starts the master server.
func (s *Server) Run(ctx context.Context) error {
	lis, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	log.Info().Str("address", s.cfg.Address).Msg("master server starting")

	// Start background workers
	go s.dispatcher.Run(ctx)
	go s.registry.MonitorHealth(ctx)

	// Handle shutdown
	go func() {
		<-ctx.Done()
		log.Info().Msg("master server shutting down")
		s.healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		s.grpcServer.GracefulStop()
	}()

	return s.grpcServer.Serve(lis)
}

// --- MasterService Implementation ---

// Submit handles job submission.
func (s *Server) Submit(ctx context.Context, req *pb.SubmitRequest) (*pb.SubmitResponse, error) {
	// Validate request
	if req.TaskType == "" {
		return nil, status.Error(codes.InvalidArgument, "task_type is required")
	}
	if len(req.Payload) == 0 {
		return nil, status.Error(codes.InvalidArgument, "payload is required")
	}

	// Generate job ID
	jobID := uuid.New().String()

	// Determine timeout
	timeout := 30 * time.Second
	if req.Timeout != nil {
		timeout = req.Timeout.AsDuration()
	}

	// Create job
	job := &Job{
		ID:        jobID,
		TaskType:  req.TaskType,
		Payload:   req.Payload,
		Priority:  int(req.Priority),
		Timeout:   timeout,
		Metadata:  req.Metadata,
		CreatedAt: time.Now(),
		Status:    types.TaskStatusPending,
	}
	s.store.SaveJob(job)

	// Create task
	task := types.NewTask(jobID, req.TaskType, req.Payload)
	task.Priority = int(req.Priority)
	task.Timeout = timeout
	task.Metadata = req.Metadata

	// Enqueue
	if err := s.scheduler.Enqueue(task); err != nil {
		return nil, status.Errorf(codes.ResourceExhausted, "queue full: %v", err)
	}

	log.Info().
		Str("job_id", jobID).
		Str("task_type", req.TaskType).
		Msg("job submitted")

	return &pb.SubmitResponse{JobId: jobID}, nil
}

// GetStatus returns the status of a job.
func (s *Server) GetStatus(ctx context.Context, req *pb.GetStatusRequest) (*pb.GetStatusResponse, error) {
	if req.JobId == "" {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	job, ok := s.store.GetJob(req.JobId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "job %s not found", req.JobId)
	}

	resp := &pb.GetStatusResponse{
		JobId:     job.ID,
		Status:    toProtoJobStatus(job.Status),
		CreatedAt: timestamppb.New(job.CreatedAt),
	}

	if job.Result != nil {
		resp.Output = job.Result.Output
		resp.Error = job.Result.Error
		resp.ExitCode = int32(job.Result.ExitCode)
		resp.WorkerId = job.Result.WorkerID
		resp.CompletedAt = timestamppb.New(job.Result.CompletedAt)
	}

	return resp, nil
}

// Cancel cancels a job.
func (s *Server) Cancel(ctx context.Context, req *pb.CancelRequest) (*pb.CancelResponse, error) {
	if req.JobId == "" {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	job, ok := s.store.GetJob(req.JobId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "job %s not found", req.JobId)
	}

	// Can only cancel pending or running jobs
	switch job.Status {
	case types.TaskStatusPending, types.TaskStatusRunning:
		s.store.UpdateJobStatus(req.JobId, types.TaskStatusCancelled)
		return &pb.CancelResponse{Cancelled: true, Message: "job cancelled"}, nil
	default:
		return &pb.CancelResponse{
			Cancelled: false,
			Message:   fmt.Sprintf("cannot cancel job in state %s", job.Status),
		}, nil
	}
}

// ListJobs returns a list of jobs.
func (s *Server) ListJobs(ctx context.Context, req *pb.ListJobsRequest) (*pb.ListJobsResponse, error) {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	offset := int(req.Offset)
	if offset < 0 {
		offset = 0
	}

	statusFilter := fromProtoJobStatus(req.StatusFilter)
	jobs := s.store.ListJobs(statusFilter, limit, offset)

	resp := &pb.ListJobsResponse{
		Total: int32(s.store.Count()),
	}

	for _, job := range jobs {
		resp.Jobs = append(resp.Jobs, &pb.JobSummary{
			JobId:     job.ID,
			TaskType:  job.TaskType,
			Status:    toProtoJobStatus(job.Status),
			CreatedAt: timestamppb.New(job.CreatedAt),
		})
	}

	return resp, nil
}

// StreamOutput streams job output (placeholder for now).
func (s *Server) StreamOutput(req *pb.StreamOutputRequest, stream pb.MasterService_StreamOutputServer) error {
	// TODO: Implement streaming output
	return status.Error(codes.Unimplemented, "streaming not yet implemented")
}

// --- RegistrationService Implementation ---

// Register handles worker registration.
func (s *Server) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	if req.WorkerId == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id is required")
	}
	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address is required")
	}

	node := NodeInfo{
		ID:           req.WorkerId,
		Address:      req.Address,
		Capabilities: req.Capabilities,
		MaxTasks:     int(req.MaxConcurrentTasks),
		Labels:       req.Labels,
	}

	if err := s.registry.Register(node); err != nil {
		return nil, status.Errorf(codes.Internal, "registration failed: %v", err)
	}

	log.Info().
		Str("worker_id", req.WorkerId).
		Str("address", req.Address).
		Strs("capabilities", req.Capabilities).
		Msg("worker registered")

	return &pb.RegisterResponse{
		Accepted:          true,
		Message:           "registration successful",
		HeartbeatInterval: durationpb.New(5 * time.Second),
	}, nil
}

// Deregister handles worker deregistration.
func (s *Server) Deregister(ctx context.Context, req *pb.DeregisterRequest) (*pb.DeregisterResponse, error) {
	if req.WorkerId == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id is required")
	}

	if err := s.registry.Deregister(req.WorkerId); err != nil {
		return nil, status.Errorf(codes.Internal, "deregistration failed: %v", err)
	}

	s.dispatcher.RemoveClient(req.WorkerId)

	log.Info().Str("worker_id", req.WorkerId).Msg("worker deregistered")

	return &pb.DeregisterResponse{Acknowledged: true}, nil
}

// Heartbeat handles worker heartbeats.
func (s *Server) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	if req.WorkerId == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id is required")
	}

	if err := s.registry.Heartbeat(req.WorkerId, int(req.ActiveTasks)); err != nil {
		// Unknown worker - tell them to re-register
		return &pb.HeartbeatResponse{
			Acknowledged: false,
			Action:       pb.HeartbeatAction_HEARTBEAT_ACTION_NONE,
		}, nil
	}

	return &pb.HeartbeatResponse{
		Acknowledged: true,
		Action:       pb.HeartbeatAction_HEARTBEAT_ACTION_NONE,
	}, nil
}

// --- Helper functions ---

func toProtoJobStatus(s types.TaskStatus) pb.JobStatus {
	switch s {
	case types.TaskStatusPending:
		return pb.JobStatus_JOB_STATUS_PENDING
	case types.TaskStatusRunning:
		return pb.JobStatus_JOB_STATUS_RUNNING
	case types.TaskStatusCompleted:
		return pb.JobStatus_JOB_STATUS_COMPLETED
	case types.TaskStatusFailed:
		return pb.JobStatus_JOB_STATUS_FAILED
	case types.TaskStatusCancelled:
		return pb.JobStatus_JOB_STATUS_CANCELLED
	case types.TaskStatusTimeout:
		return pb.JobStatus_JOB_STATUS_TIMEOUT
	default:
		return pb.JobStatus_JOB_STATUS_UNSPECIFIED
	}
}

func fromProtoJobStatus(s pb.JobStatus) types.TaskStatus {
	switch s {
	case pb.JobStatus_JOB_STATUS_PENDING:
		return types.TaskStatusPending
	case pb.JobStatus_JOB_STATUS_RUNNING:
		return types.TaskStatusRunning
	case pb.JobStatus_JOB_STATUS_COMPLETED:
		return types.TaskStatusCompleted
	case pb.JobStatus_JOB_STATUS_FAILED:
		return types.TaskStatusFailed
	case pb.JobStatus_JOB_STATUS_CANCELLED:
		return types.TaskStatusCancelled
	case pb.JobStatus_JOB_STATUS_TIMEOUT:
		return types.TaskStatusTimeout
	default:
		return 0
	}
}
