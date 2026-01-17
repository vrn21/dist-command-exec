package master

import (
	"sync"
	"time"

	"dist-command-exec/pkg/types"
)

// Store provides in-memory job state storage.
type Store struct {
	mu      sync.RWMutex
	jobs    map[string]*Job
	results map[string]*types.Result
}

// Job represents a submitted job with its result.
type Job struct {
	ID        string
	TaskType  string
	Payload   []byte
	Priority  int
	Timeout   time.Duration
	Metadata  map[string]string
	CreatedAt time.Time
	Status    types.TaskStatus
	Result    *types.Result
}

// NewStore creates a new job store.
func NewStore() *Store {
	return &Store{
		jobs:    make(map[string]*Job),
		results: make(map[string]*types.Result),
	}
}

// SaveJob stores a job.
func (s *Store) SaveJob(job *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
}

// GetJob retrieves a job by ID.
func (s *Store) GetJob(id string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	return job, ok
}

// UpdateJobStatus updates the status of a job.
func (s *Store) UpdateJobStatus(id string, status types.TaskStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[id]; ok {
		job.Status = status
	}
}

// SaveResult stores a job result.
func (s *Store) SaveResult(result *types.Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[result.TaskID] = result

	// Also update job status
	if job, ok := s.jobs[result.TaskID]; ok {
		job.Result = result
		if result.IsSuccess() {
			job.Status = types.TaskStatusCompleted
		} else {
			job.Status = types.TaskStatusFailed
		}
	}
}

// GetResult retrieves a job result.
func (s *Store) GetResult(jobID string) (*types.Result, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result, ok := s.results[jobID]
	return result, ok
}

// ListJobs returns jobs filtered by status.
func (s *Store) ListJobs(statusFilter types.TaskStatus, limit, offset int) []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Job
	i := 0
	for _, job := range s.jobs {
		if statusFilter != 0 && job.Status != statusFilter {
			continue
		}
		if i < offset {
			i++
			continue
		}
		result = append(result, job)
		if len(result) >= limit {
			break
		}
		i++
	}
	return result
}

// Count returns the total number of jobs.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.jobs)
}

// CountByStatus returns job counts grouped by status.
func (s *Store) CountByStatus() map[types.TaskStatus]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	counts := make(map[types.TaskStatus]int)
	for _, job := range s.jobs {
		counts[job.Status]++
	}
	return counts
}
