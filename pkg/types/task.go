// Package types defines shared data structures for the distributed command execution system.
package types

import (
	"time"
)

// TaskStatus represents the lifecycle state of a task.
type TaskStatus int

const (
	TaskStatusCreated TaskStatus = iota
	TaskStatusPending
	TaskStatusRunning
	TaskStatusCompleted
	TaskStatusFailed
	TaskStatusCancelled
	TaskStatusTimeout
)

// String returns the string representation of a TaskStatus.
func (s TaskStatus) String() string {
	switch s {
	case TaskStatusCreated:
		return "CREATED"
	case TaskStatusPending:
		return "PENDING"
	case TaskStatusRunning:
		return "RUNNING"
	case TaskStatusCompleted:
		return "COMPLETED"
	case TaskStatusFailed:
		return "FAILED"
	case TaskStatusCancelled:
		return "CANCELLED"
	case TaskStatusTimeout:
		return "TIMEOUT"
	default:
		return "UNKNOWN"
	}
}

// Task represents a unit of work to be executed.
type Task struct {
	// ID is the unique identifier for this task.
	ID string

	// Type indicates the executor type (e.g., "command", "http").
	Type string

	// Payload contains task-specific data (JSON encoded).
	Payload []byte

	// Priority determines execution order (higher = more urgent).
	Priority int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// Retries is the current retry count.
	Retries int

	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int

	// CreatedAt is when the task was submitted.
	CreatedAt time.Time

	// StartedAt is when execution began (nil if not started).
	StartedAt *time.Time

	// CompletedAt is when execution finished (nil if not complete).
	CompletedAt *time.Time

	// Status is the current task state.
	Status TaskStatus

	// WorkerID is the assigned worker (empty if unassigned).
	WorkerID string

	// Metadata contains arbitrary key-value pairs.
	Metadata map[string]string
}

// NewTask creates a new task with default values.
func NewTask(id, taskType string, payload []byte) *Task {
	return &Task{
		ID:         id,
		Type:       taskType,
		Payload:    payload,
		Priority:   0,
		Timeout:    30 * time.Second,
		Retries:    0,
		MaxRetries: 3,
		CreatedAt:  time.Now(),
		Status:     TaskStatusCreated,
		Metadata:   make(map[string]string),
	}
}

// IsTerminal returns true if the task is in a terminal state.
func (t *Task) IsTerminal() bool {
	switch t.Status {
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled, TaskStatusTimeout:
		return true
	default:
		return false
	}
}

// CanRetry returns true if the task can be retried.
func (t *Task) CanRetry() bool {
	return t.Retries < t.MaxRetries && t.Status == TaskStatusFailed
}
