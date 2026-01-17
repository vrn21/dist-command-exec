// Package executor defines the interface for task execution.
// This package is public and can be imported to build custom executors.
package executor

import (
	"context"
)

// Executor defines the contract for all workload executors.
// This is the KEY interface that enables modularity.
type Executor interface {
	// Name returns the executor type name (e.g., "command", "http").
	Name() string

	// Execute runs the workload and returns the result.
	// ctx includes timeout/cancellation.
	// task contains the workload payload.
	Execute(ctx context.Context, task Task) (Result, error)

	// Validate checks if a task payload is valid for this executor.
	// Called before enqueueing to fail fast.
	Validate(payload []byte) error

	// HealthCheck verifies the executor can operate.
	// Called during worker health checks.
	HealthCheck(ctx context.Context) error
}

// Task represents work to be executed.
type Task struct {
	// ID is the unique task identifier.
	ID string

	// Type is the executor type (matches Executor.Name()).
	Type string

	// Payload is executor-specific data (typically JSON).
	Payload []byte

	// Metadata is optional context for the executor.
	Metadata map[string]string
}

// Result is the outcome of task execution.
type Result struct {
	// Output is executor-specific output.
	Output []byte

	// ExitCode is meaningful for command executor, 0 for others.
	ExitCode int

	// Error is the error message if any.
	Error string

	// Metadata is executor-specific metadata.
	Metadata map[string]string
}

// NewResult creates a new result with defaults.
func NewResult() Result {
	return Result{
		Metadata: make(map[string]string),
	}
}

// Success creates a successful result with output.
func Success(output []byte) Result {
	return Result{
		Output:   output,
		ExitCode: 0,
		Metadata: make(map[string]string),
	}
}

// Failure creates a failed result with an error message.
func Failure(err string, exitCode int) Result {
	return Result{
		Error:    err,
		ExitCode: exitCode,
		Metadata: make(map[string]string),
	}
}
