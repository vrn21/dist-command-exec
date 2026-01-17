package master

import (
	"context"
	"errors"
	"sync"
	"time"

	"dist-command-exec/pkg/types"
)

// Common errors.
var (
	ErrQueueFull    = errors.New("task queue is full")
	ErrTaskExists   = errors.New("task already exists")
	ErrTaskNotFound = errors.New("task not found")
)

// Scheduler manages task queuing and lifecycle.
type Scheduler struct {
	mu       sync.RWMutex
	pending  chan *types.Task
	tasks    map[string]*types.Task // All tasks by ID
	maxQueue int
}

// NewScheduler creates a new task scheduler.
func NewScheduler(maxQueueSize int) *Scheduler {
	if maxQueueSize <= 0 {
		maxQueueSize = 10000
	}
	return &Scheduler{
		pending:  make(chan *types.Task, maxQueueSize),
		tasks:    make(map[string]*types.Task),
		maxQueue: maxQueueSize,
	}
}

// Enqueue adds a task to the pending queue.
func (s *Scheduler) Enqueue(task *types.Task) error {
	s.mu.Lock()
	if _, exists := s.tasks[task.ID]; exists {
		s.mu.Unlock()
		return ErrTaskExists
	}
	task.Status = types.TaskStatusPending
	s.tasks[task.ID] = task
	s.mu.Unlock()

	select {
	case s.pending <- task:
		return nil
	default:
		s.mu.Lock()
		delete(s.tasks, task.ID)
		s.mu.Unlock()
		return ErrQueueFull
	}
}

// Dequeue returns the next pending task (blocks until available).
func (s *Scheduler) Dequeue(ctx context.Context) (*types.Task, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case task := <-s.pending:
		return task, nil
	}
}

// MarkRunning marks a task as running on a worker.
func (s *Scheduler) MarkRunning(taskID, workerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}

	now := time.Now()
	task.Status = types.TaskStatusRunning
	task.StartedAt = &now
	task.WorkerID = workerID
	return nil
}

// MarkComplete marks a task as completed.
func (s *Scheduler) MarkComplete(taskID string, result *types.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}

	now := time.Now()
	task.Status = types.TaskStatusCompleted
	task.CompletedAt = &now
	return nil
}

// MarkFailed marks a task as failed.
func (s *Scheduler) MarkFailed(taskID string, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}

	now := time.Now()
	task.Status = types.TaskStatusFailed
	task.CompletedAt = &now
	return nil
}

// Requeue puts a failed task back in the pending queue for retry.
func (s *Scheduler) Requeue(task *types.Task) error {
	s.mu.Lock()
	task.Retries++
	task.Status = types.TaskStatusPending
	task.WorkerID = ""
	task.StartedAt = nil
	s.mu.Unlock()

	select {
	case s.pending <- task:
		return nil
	default:
		return ErrQueueFull
	}
}

// GetTask returns a task by ID.
func (s *Scheduler) GetTask(id string) (*types.Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	return t, ok
}

// PendingCount returns the number of pending tasks.
func (s *Scheduler) PendingCount() int {
	return len(s.pending)
}

// ListTasks returns tasks filtered by status (0 = all).
func (s *Scheduler) ListTasks(statusFilter types.TaskStatus, limit, offset int) []*types.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*types.Task
	i := 0
	for _, task := range s.tasks {
		if statusFilter != 0 && task.Status != statusFilter {
			continue
		}
		if i < offset {
			i++
			continue
		}
		result = append(result, task)
		if len(result) >= limit {
			break
		}
		i++
	}
	return result
}

// Run starts the scheduler dispatch loop.
func (s *Scheduler) Run(ctx context.Context, dispatch func(*types.Task)) {
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-s.pending:
			dispatch(task)
		}
	}
}
