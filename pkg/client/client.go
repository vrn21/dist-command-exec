// Package client provides a Go client for the master service.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/durationpb"

	pb "dist-command-exec/gen/go/distexec/v1"
	"dist-command-exec/pkg/executor"
)

// Client provides a high-level Go API for the master service.
type Client struct {
	conn   *grpc.ClientConn
	master pb.MasterServiceClient
}

// New creates a new client connected to the master at the given address.
func New(address string) (*Client, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return &Client{
		conn:   conn,
		master: pb.NewMasterServiceClient(conn),
	}, nil
}

// Close closes the connection to the master.
func (c *Client) Close() error {
	return c.conn.Close()
}

// RunCommand submits a command for execution and returns the job ID.
func (c *Client) RunCommand(ctx context.Context, command string, args ...string) (string, error) {
	return c.RunCommandWithTimeout(ctx, command, 30*time.Second, args...)
}

// RunCommandWithTimeout submits a command with a specified timeout.
func (c *Client) RunCommandWithTimeout(ctx context.Context, command string, timeout time.Duration, args ...string) (string, error) {
	payload := executor.CommandPayload{
		Command: command,
		Args:    args,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	resp, err := c.master.Submit(ctx, &pb.SubmitRequest{
		TaskType: "command",
		Payload:  payloadBytes,
		Timeout:  durationpb.New(timeout),
	})
	if err != nil {
		return "", fmt.Errorf("submit: %w", err)
	}

	return resp.JobId, nil
}

// GetStatus returns the status of a job.
func (c *Client) GetStatus(ctx context.Context, jobID string) (*JobStatus, error) {
	resp, err := c.master.GetStatus(ctx, &pb.GetStatusRequest{JobId: jobID})
	if err != nil {
		return nil, fmt.Errorf("get status: %w", err)
	}

	return &JobStatus{
		JobID:       resp.JobId,
		Status:      resp.Status.String(),
		Output:      resp.Output,
		Error:       resp.Error,
		ExitCode:    int(resp.ExitCode),
		WorkerID:    resp.WorkerId,
		CreatedAt:   resp.CreatedAt.AsTime(),
		CompletedAt: resp.CompletedAt.AsTime(),
	}, nil
}

// Cancel cancels a job.
func (c *Client) Cancel(ctx context.Context, jobID string) (bool, error) {
	resp, err := c.master.Cancel(ctx, &pb.CancelRequest{JobId: jobID})
	if err != nil {
		return false, fmt.Errorf("cancel: %w", err)
	}
	return resp.Cancelled, nil
}

// WaitForCompletion polls until the job completes or the context is cancelled.
func (c *Client) WaitForCompletion(ctx context.Context, jobID string, pollInterval time.Duration) (*JobStatus, error) {
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			status, err := c.GetStatus(ctx, jobID)
			if err != nil {
				return nil, err
			}

			switch status.Status {
			case "JOB_STATUS_COMPLETED", "JOB_STATUS_FAILED", "JOB_STATUS_CANCELLED", "JOB_STATUS_TIMEOUT":
				return status, nil
			}
		}
	}
}

// RunAndWait submits a command and waits for completion.
func (c *Client) RunAndWait(ctx context.Context, command string, args ...string) (*JobStatus, error) {
	jobID, err := c.RunCommand(ctx, command, args...)
	if err != nil {
		return nil, err
	}
	return c.WaitForCompletion(ctx, jobID, 0)
}

// JobStatus represents the status of a job.
type JobStatus struct {
	JobID       string
	Status      string
	Output      []byte
	Error       string
	ExitCode    int
	WorkerID    string
	CreatedAt   time.Time
	CompletedAt time.Time
}

// IsComplete returns true if the job is in a terminal state.
func (s *JobStatus) IsComplete() bool {
	switch s.Status {
	case "JOB_STATUS_COMPLETED", "JOB_STATUS_FAILED", "JOB_STATUS_CANCELLED", "JOB_STATUS_TIMEOUT":
		return true
	default:
		return false
	}
}

// IsSuccess returns true if the job completed successfully.
func (s *JobStatus) IsSuccess() bool {
	return s.Status == "JOB_STATUS_COMPLETED" && s.ExitCode == 0
}
