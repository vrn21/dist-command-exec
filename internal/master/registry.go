package master

import (
	"context"
	"sync"
	"time"
)

// NodeStatus represents the status of a registered worker node.
type NodeStatus int

const (
	NodeStatusHealthy NodeStatus = iota
	NodeStatusUnhealthy
	NodeStatusDraining
)

// String returns the string representation of NodeStatus.
func (s NodeStatus) String() string {
	switch s {
	case NodeStatusHealthy:
		return "HEALTHY"
	case NodeStatusUnhealthy:
		return "UNHEALTHY"
	case NodeStatusDraining:
		return "DRAINING"
	default:
		return "UNKNOWN"
	}
}

// NodeInfo holds information about a registered worker node.
type NodeInfo struct {
	ID           string
	Address      string // "host:port"
	Capabilities []string
	MaxTasks     int
	ActiveTasks  int
	LastSeen     time.Time
	Status       NodeStatus
	Labels       map[string]string
}

// AvailableCapacity returns how many more tasks this node can accept.
func (n *NodeInfo) AvailableCapacity() int {
	return n.MaxTasks - n.ActiveTasks
}

// NodeRegistry tracks available worker nodes and their health.
type NodeRegistry struct {
	mu               sync.RWMutex
	nodes            map[string]*NodeInfo
	heartbeatTimeout time.Duration
	unhealthyTimeout time.Duration
	onNodeRemoved    func(nodeID string)
}

// NewNodeRegistry creates a new node registry.
func NewNodeRegistry(heartbeatTimeout, unhealthyTimeout time.Duration) *NodeRegistry {
	if heartbeatTimeout <= 0 {
		heartbeatTimeout = 15 * time.Second
	}
	if unhealthyTimeout <= 0 {
		unhealthyTimeout = 30 * time.Second
	}
	return &NodeRegistry{
		nodes:            make(map[string]*NodeInfo),
		heartbeatTimeout: heartbeatTimeout,
		unhealthyTimeout: unhealthyTimeout,
	}
}

// SetNodeRemovedCallback sets a callback for when a node is removed.
func (r *NodeRegistry) SetNodeRemovedCallback(callback func(nodeID string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onNodeRemoved = callback
}

// Register adds a new worker node.
func (r *NodeRegistry) Register(node NodeInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node.LastSeen = time.Now()
	node.Status = NodeStatusHealthy
	r.nodes[node.ID] = &node
	return nil
}

// Deregister removes a worker node.
func (r *NodeRegistry) Deregister(nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.nodes, nodeID)
	return nil
}

// Heartbeat updates the last seen time and load for a node.
func (r *NodeRegistry) Heartbeat(nodeID string, activeTasks int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, ok := r.nodes[nodeID]
	if !ok {
		return ErrTaskNotFound // TODO: better error
	}

	node.LastSeen = time.Now()
	node.ActiveTasks = activeTasks
	if node.Status == NodeStatusUnhealthy {
		node.Status = NodeStatusHealthy
	}
	return nil
}

// GetNode returns a single node by ID.
func (r *NodeRegistry) GetNode(nodeID string) (*NodeInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	node, ok := r.nodes[nodeID]
	return node, ok
}

// GetHealthyNodes returns all healthy nodes.
func (r *NodeRegistry) GetHealthyNodes() []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var healthy []NodeInfo
	for _, node := range r.nodes {
		if node.Status == NodeStatusHealthy {
			healthy = append(healthy, *node)
		}
	}
	return healthy
}

// GetAllNodes returns all registered nodes.
func (r *NodeRegistry) GetAllNodes() []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodes := make([]NodeInfo, 0, len(r.nodes))
	for _, node := range r.nodes {
		nodes = append(nodes, *node)
	}
	return nodes
}

// NodeCount returns the total number of registered nodes.
func (r *NodeRegistry) NodeCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.nodes)
}

// HealthyNodeCount returns the number of healthy nodes.
func (r *NodeRegistry) HealthyNodeCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, node := range r.nodes {
		if node.Status == NodeStatusHealthy {
			count++
		}
	}
	return count
}

// IncrementActiveTasks increases the active task count for a node.
func (r *NodeRegistry) IncrementActiveTasks(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if node, ok := r.nodes[nodeID]; ok {
		node.ActiveTasks++
	}
}

// DecrementActiveTasks decreases the active task count for a node.
func (r *NodeRegistry) DecrementActiveTasks(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if node, ok := r.nodes[nodeID]; ok && node.ActiveTasks > 0 {
		node.ActiveTasks--
	}
}

// MonitorHealth runs a background health check loop.
func (r *NodeRegistry) MonitorHealth(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.checkHealth()
		}
	}
}

func (r *NodeRegistry) checkHealth() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	var toRemove []string

	for id, node := range r.nodes {
		timeSinceLastSeen := now.Sub(node.LastSeen)

		if timeSinceLastSeen > r.unhealthyTimeout {
			toRemove = append(toRemove, id)
		} else if timeSinceLastSeen > r.heartbeatTimeout && node.Status == NodeStatusHealthy {
			node.Status = NodeStatusUnhealthy
		}
	}

	for _, id := range toRemove {
		delete(r.nodes, id)
		if r.onNodeRemoved != nil {
			r.onNodeRemoved(id)
		}
	}
}
