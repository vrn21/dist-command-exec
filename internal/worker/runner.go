package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"dist-command-exec/pkg/executor"
)

// TaskRunner executes tasks using the executor registry.
type TaskRunner struct {
	registry    *executor.Registry
	maxTasks    int
	activeTasks int64

	// Track running tasks for graceful shutdown
	mu      sync.Mutex
	running map[string]context.CancelFunc
	wg      sync.WaitGroup
}

// NewTaskRunner creates a new task runner.
func NewTaskRunner(registry *executor.Registry, maxConcurrentTasks int) *TaskRunner {
	if maxConcurrentTasks <= 0 {
		maxConcurrentTasks = 10
	}
	return &TaskRunner{
		registry: registry,
		maxTasks: maxConcurrentTasks,
		running:  make(map[string]context.CancelFunc),
	}
}

// Run executes a task and returns the result.
func (r *TaskRunner) Run(ctx context.Context, task executor.Task, timeout time.Duration) (executor.Result, error) {
	// Check capacity
	if int(atomic.LoadInt64(&r.activeTasks)) >= r.maxTasks {
		return executor.Result{}, ErrAtCapacity
	}

	// Create cancellable context with timeout
	execCtx, cancel := context.WithTimeout(ctx, timeout)

	// Track the running task
	r.mu.Lock()
	r.running[task.ID] = cancel
	r.mu.Unlock()
	atomic.AddInt64(&r.activeTasks, 1)
	r.wg.Add(1)

	defer func() {
		r.mu.Lock()
		delete(r.running, task.ID)
		r.mu.Unlock()
		atomic.AddInt64(&r.activeTasks, -1)
		r.wg.Done()
		cancel()
	}()

	log.Debug().
		Str("task_id", task.ID).
		Str("task_type", task.Type).
		Msg("executing task")

	start := time.Now()
	result, err := r.registry.Execute(execCtx, task)
	duration := time.Since(start)

	if err != nil {
		log.Error().
			Err(err).
			Str("task_id", task.ID).
			Dur("duration", duration).
			Msg("task execution failed")
		return result, err
	}

	log.Debug().
		Str("task_id", task.ID).
		Int("exit_code", result.ExitCode).
		Dur("duration", duration).
		Msg("task completed")

	return result, nil
}

// CancelTask cancels a running task.
func (r *TaskRunner) CancelTask(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cancel, ok := r.running[taskID]; ok {
		cancel()
		return true
	}
	return false
}

// ActiveCount returns the number of active tasks.
func (r *TaskRunner) ActiveCount() int {
	return int(atomic.LoadInt64(&r.activeTasks))
}

// MaxTasks returns the maximum concurrent tasks.
func (r *TaskRunner) MaxTasks() int {
	return r.maxTasks
}

// WaitForDrain waits for all running tasks to complete.
func (r *TaskRunner) WaitForDrain(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		// Cancel all running tasks
		r.mu.Lock()
		for _, cancel := range r.running {
			cancel()
		}
		r.mu.Unlock()
		// Wait again
		r.wg.Wait()
	case <-done:
		// All tasks completed naturally
	}
}

// Capabilities returns the executor capabilities.
func (r *TaskRunner) Capabilities() []string {
	return r.registry.Capabilities()
}

// HealthCheck runs health checks on all executors.
func (r *TaskRunner) HealthCheck(ctx context.Context) map[string]error {
	return r.registry.HealthCheckAll(ctx)
}
