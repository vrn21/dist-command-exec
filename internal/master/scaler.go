package master

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// AutoScaler manages worker node scaling.
// In production, this would call the Kubernetes API.
// For local development, this is a no-op or mock implementation.
type AutoScaler struct {
	registry *NodeRegistry
	cfg      ScalerConfig

	mu          sync.Mutex
	lastScaleUp time.Time
	enabled     bool

	// K8s API client would go here
	// k8s *kubernetes.Clientset
}

// ScalerConfig holds auto-scaler configuration.
type ScalerConfig struct {
	// MinReplicas is the minimum number of workers.
	MinReplicas int

	// MaxReplicas is the maximum number of workers.
	MaxReplicas int

	// TasksPerWorker is the target tasks per worker.
	TasksPerWorker int

	// ScaleUpThreshold is pending tasks per worker that triggers scale up.
	ScaleUpThreshold int

	// CooldownPeriod is the minimum time between scaling operations.
	CooldownPeriod time.Duration

	// ScaleDownDelay is how long a worker must be idle before scale down.
	ScaleDownDelay time.Duration

	// Enabled controls whether scaling is active.
	Enabled bool
}

// DefaultScalerConfig returns default scaler configuration.
func DefaultScalerConfig() ScalerConfig {
	return ScalerConfig{
		MinReplicas:      3,
		MaxReplicas:      20,
		TasksPerWorker:   10,
		ScaleUpThreshold: 5,
		CooldownPeriod:   30 * time.Second,
		ScaleDownDelay:   5 * time.Minute,
		Enabled:          false, // Disabled by default for local dev
	}
}

// NewAutoScaler creates a new auto-scaler.
func NewAutoScaler(registry *NodeRegistry, cfg ScalerConfig) *AutoScaler {
	return &AutoScaler{
		registry: registry,
		cfg:      cfg,
		enabled:  cfg.Enabled,
	}
}

// EnsureCapacity scales workers if needed for pending tasks.
func (s *AutoScaler) EnsureCapacity(ctx context.Context, pendingTasks int) error {
	if !s.enabled {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Respect cooldown
	if time.Since(s.lastScaleUp) < s.cfg.CooldownPeriod {
		return nil
	}

	nodes := s.registry.GetHealthyNodes()
	currentReplicas := len(nodes)

	// Calculate available capacity
	totalCapacity := 0
	for _, n := range nodes {
		totalCapacity += n.AvailableCapacity()
	}

	// CASE 1: No capacity and tasks pending → Scale up immediately
	if totalCapacity == 0 && pendingTasks > 0 {
		needed := min((pendingTasks/s.cfg.TasksPerWorker)+1, s.cfg.MaxReplicas-currentReplicas)
		if needed > 0 {
			log.Info().
				Int("pending", pendingTasks).
				Int("current_workers", currentReplicas).
				Int("needed", needed).
				Msg("scaling up: no capacity")
			return s.scaleUp(ctx, needed)
		}
	}

	// CASE 2: Queue backing up → Proactive scale up
	if pendingTasks > currentReplicas*s.cfg.ScaleUpThreshold {
		if currentReplicas < s.cfg.MaxReplicas {
			log.Info().
				Int("pending", pendingTasks).
				Int("current_workers", currentReplicas).
				Msg("scaling up: queue backing up")
			return s.scaleUp(ctx, 1)
		}
	}

	return nil
}

// scaleUp adds N workers.
func (s *AutoScaler) scaleUp(ctx context.Context, count int) error {
	// In production: call Kubernetes API
	// For now: just log the intent
	log.Info().Int("count", count).Msg("would scale up workers (mock)")
	s.lastScaleUp = time.Now()

	// Production implementation would look like:
	// deploy, err := s.k8s.AppsV1().Deployments(s.namespace).Get(ctx, s.deployment, metav1.GetOptions{})
	// if err != nil {
	//     return fmt.Errorf("get deployment: %w", err)
	// }
	// newReplicas := *deploy.Spec.Replicas + int32(count)
	// deploy.Spec.Replicas = &newReplicas
	// _, err = s.k8s.AppsV1().Deployments(s.namespace).Update(ctx, deploy, metav1.UpdateOptions{})
	// return err

	return nil
}

// ScaleDown removes idle workers.
func (s *AutoScaler) ScaleDown(ctx context.Context) error {
	if !s.enabled {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	nodes := s.registry.GetHealthyNodes()
	currentReplicas := len(nodes)

	// Don't scale below minimum
	if currentReplicas <= s.cfg.MinReplicas {
		return nil
	}

	// Count idle workers
	idleCount := 0
	for _, n := range nodes {
		if n.ActiveTasks == 0 {
			idleCount++
		}
	}

	// If more than half are idle, scale down by 1
	if idleCount > currentReplicas/2 {
		log.Info().
			Int("idle_workers", idleCount).
			Int("current_workers", currentReplicas).
			Msg("would scale down workers (mock)")
		// Production: update deployment replicas
	}

	return nil
}

// GetCurrentScale returns current and desired replica count.
func (s *AutoScaler) GetCurrentScale(ctx context.Context) (current, desired int, err error) {
	current = s.registry.HealthyNodeCount()
	desired = current // In mock mode, desired == current
	return current, desired, nil
}

// SetEnabled enables or disables auto-scaling.
func (s *AutoScaler) SetEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = enabled
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
