package types

import (
	"time"
)

// ResultStatus represents the outcome of task execution.
type ResultStatus int

const (
	ResultStatusSuccess ResultStatus = iota
	ResultStatusFailure
	ResultStatusTimeout
	ResultStatusCancelled
)

// String returns the string representation of a ResultStatus.
func (s ResultStatus) String() string {
	switch s {
	case ResultStatusSuccess:
		return "SUCCESS"
	case ResultStatusFailure:
		return "FAILURE"
	case ResultStatusTimeout:
		return "TIMEOUT"
	case ResultStatusCancelled:
		return "CANCELLED"
	default:
		return "UNKNOWN"
	}
}

// Result represents the outcome of task execution.
type Result struct {
	// TaskID is the identifier of the executed task.
	TaskID string

	// Status is the execution outcome.
	Status ResultStatus

	// Output is the task output (stdout for commands).
	Output []byte

	// Error is the error message if execution failed.
	Error string

	// ExitCode is the process exit code (for command tasks).
	ExitCode int

	// Duration is the actual execution time.
	Duration time.Duration

	// WorkerID is the worker that executed the task.
	WorkerID string

	// CompletedAt is when execution finished.
	CompletedAt time.Time

	// Metadata contains additional executor-specific data.
	Metadata map[string]string
}

// NewResult creates a new result with default values.
func NewResult(taskID string) *Result {
	return &Result{
		TaskID:      taskID,
		Status:      ResultStatusSuccess,
		CompletedAt: time.Now(),
		Metadata:    make(map[string]string),
	}
}

// Success creates a successful result.
func Success(taskID string, output []byte, duration time.Duration) *Result {
	return &Result{
		TaskID:      taskID,
		Status:      ResultStatusSuccess,
		Output:      output,
		ExitCode:    0,
		Duration:    duration,
		CompletedAt: time.Now(),
		Metadata:    make(map[string]string),
	}
}

// Failure creates a failed result.
func Failure(taskID string, err string, exitCode int, duration time.Duration) *Result {
	return &Result{
		TaskID:      taskID,
		Status:      ResultStatusFailure,
		Error:       err,
		ExitCode:    exitCode,
		Duration:    duration,
		CompletedAt: time.Now(),
		Metadata:    make(map[string]string),
	}
}

// IsSuccess returns true if the result indicates success.
func (r *Result) IsSuccess() bool {
	return r.Status == ResultStatusSuccess
}
