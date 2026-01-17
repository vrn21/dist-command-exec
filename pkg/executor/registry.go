package executor

import (
	"context"
	"fmt"
	"sync"
)

// Registry manages executor instances.
type Registry struct {
	mu        sync.RWMutex
	executors map[string]Executor
}

// NewRegistry creates a new executor registry.
func NewRegistry() *Registry {
	return &Registry{
		executors: make(map[string]Executor),
	}
}

// Register adds an executor to the registry.
func (r *Registry) Register(e Executor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executors[e.Name()] = e
}

// Get retrieves an executor by name.
func (r *Registry) Get(name string) (Executor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.executors[name]
	return e, ok
}

// Capabilities returns all registered executor types.
func (r *Registry) Capabilities() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	caps := make([]string, 0, len(r.executors))
	for name := range r.executors {
		caps = append(caps, name)
	}
	return caps
}

// Execute routes a task to the appropriate executor.
func (r *Registry) Execute(ctx context.Context, task Task) (Result, error) {
	executor, ok := r.Get(task.Type)
	if !ok {
		return Result{}, fmt.Errorf("unknown executor type: %s", task.Type)
	}
	return executor.Execute(ctx, task)
}

// Validate validates a task payload using the appropriate executor.
func (r *Registry) Validate(taskType string, payload []byte) error {
	executor, ok := r.Get(taskType)
	if !ok {
		return fmt.Errorf("unknown executor type: %s", taskType)
	}
	return executor.Validate(payload)
}

// HealthCheckAll runs health checks on all executors.
func (r *Registry) HealthCheckAll(ctx context.Context) map[string]error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	results := make(map[string]error)
	for name, e := range r.executors {
		results[name] = e.HealthCheck(ctx)
	}
	return results
}

// NewDefaultRegistry creates a registry with the default executors.
func NewDefaultRegistry() *Registry {
	reg := NewRegistry()
	reg.Register(NewCommandExecutor())
	return reg
}
