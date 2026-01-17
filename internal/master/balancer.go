package master

import (
	"errors"
	"sync"
	"sync/atomic"

	"dist-command-exec/pkg/types"
)

// Common errors.
var (
	ErrNoHealthyNodes = errors.New("no healthy nodes available")
)

// LoadBalancer defines the interface for node selection.
type LoadBalancer interface {
	// Select chooses a node for task execution.
	Select(task *types.Task, nodes []NodeInfo) (NodeInfo, error)
}

// RoundRobinBalancer cycles through available nodes sequentially.
type RoundRobinBalancer struct {
	counter uint64
}

// NewRoundRobinBalancer creates a new round-robin balancer.
func NewRoundRobinBalancer() *RoundRobinBalancer {
	return &RoundRobinBalancer{}
}

// Select picks the next node in round-robin order.
func (b *RoundRobinBalancer) Select(task *types.Task, nodes []NodeInfo) (NodeInfo, error) {
	if len(nodes) == 0 {
		return NodeInfo{}, ErrNoHealthyNodes
	}

	idx := atomic.AddUint64(&b.counter, 1) % uint64(len(nodes))
	return nodes[idx], nil
}

// LeastConnectionsBalancer picks the node with fewest active tasks.
type LeastConnectionsBalancer struct {
	mu sync.RWMutex
}

// NewLeastConnectionsBalancer creates a new least-connections balancer.
func NewLeastConnectionsBalancer() *LeastConnectionsBalancer {
	return &LeastConnectionsBalancer{}
}

// Select picks the node with the most available capacity.
func (b *LeastConnectionsBalancer) Select(task *types.Task, nodes []NodeInfo) (NodeInfo, error) {
	if len(nodes) == 0 {
		return NodeInfo{}, ErrNoHealthyNodes
	}

	var best NodeInfo
	minLoad := int(^uint(0) >> 1) // Max int

	for _, n := range nodes {
		if n.ActiveTasks < minLoad {
			minLoad = n.ActiveTasks
			best = n
		}
	}

	return best, nil
}

// WeightedRoundRobinBalancer distributes load proportionally based on MaxTasks.
type WeightedRoundRobinBalancer struct {
	mu       sync.Mutex
	counter  uint64
	weights  []int
	nodeIDs  []string
	position int
	current  int
}

// NewWeightedRoundRobinBalancer creates a new weighted round-robin balancer.
func NewWeightedRoundRobinBalancer() *WeightedRoundRobinBalancer {
	return &WeightedRoundRobinBalancer{}
}

// Select picks a node based on weighted round-robin.
func (b *WeightedRoundRobinBalancer) Select(task *types.Task, nodes []NodeInfo) (NodeInfo, error) {
	if len(nodes) == 0 {
		return NodeInfo{}, ErrNoHealthyNodes
	}

	// Simple weighted selection: use available capacity as weight
	var totalWeight int
	for _, n := range nodes {
		totalWeight += n.AvailableCapacity()
	}

	if totalWeight == 0 {
		// All nodes at capacity, fall back to round-robin
		idx := atomic.AddUint64(&b.counter, 1) % uint64(len(nodes))
		return nodes[idx], nil
	}

	// Select based on weight
	target := int(atomic.AddUint64(&b.counter, 1) % uint64(totalWeight))
	for _, n := range nodes {
		target -= n.AvailableCapacity()
		if target < 0 {
			return n, nil
		}
	}

	// Fallback (shouldn't reach here)
	return nodes[0], nil
}

// RandomBalancer selects a random healthy node.
type RandomBalancer struct {
	counter uint64
}

// NewRandomBalancer creates a new random balancer.
func NewRandomBalancer() *RandomBalancer {
	return &RandomBalancer{}
}

// Select picks a pseudo-random node.
func (b *RandomBalancer) Select(task *types.Task, nodes []NodeInfo) (NodeInfo, error) {
	if len(nodes) == 0 {
		return NodeInfo{}, ErrNoHealthyNodes
	}

	// Use counter as pseudo-random (simple for now, could use real random)
	idx := atomic.AddUint64(&b.counter, 1) % uint64(len(nodes))
	return nodes[idx], nil
}
