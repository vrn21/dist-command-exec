# Component Breakdown

**Parent:** [00-OVERVIEW.md](./00-OVERVIEW.md)

---

## System Components

This document details each component, its responsibilities, and internal structure.

---

## 1. Master Node

The brain of the system. Single point of coordination (HA can be added later with leader election).

### 1.1 Request Handler

**Responsibility:** Accept and validate incoming execution requests.

```
Client Request → Validation → Rate Limiting → Enqueue
```

**Key Behaviors:**
- Expose gRPC service for job submission
- Validate command syntax/safety (basic for PoC)
- Assign unique job IDs (UUIDs)
- Return job handle for status polling

**Interface Sketch:**

```go
type RequestHandler interface {
    Submit(ctx context.Context, req *SubmitRequest) (*JobHandle, error)
    GetStatus(ctx context.Context, jobID string) (*JobStatus, error)
    Cancel(ctx context.Context, jobID string) error
}
```

### 1.2 Task Scheduler

**Responsibility:** Queue tasks and determine execution order.

```
Job Queue → Priority Sorting → Ready Queue → Dispatch
```

**Key Behaviors:**
- In-memory queue (simple for PoC, can add Redis/NATS later)
- FIFO by default, priority support for future
- Mark tasks as: PENDING → RUNNING → COMPLETED/FAILED
- Timeout handling

**Interface Sketch:**

```go
type Scheduler interface {
    Enqueue(task Task) error
    Dequeue() (Task, error)  // Blocks until task available
    MarkComplete(taskID string, result Result) error
    MarkFailed(taskID string, err error) error
}
```

### 1.3 Node Registry

**Responsibility:** Track available worker nodes and their health.

```
Worker Heartbeat → Registry Update → Selection Pool
```

**Key Behaviors:**
- Workers register on startup via gRPC
- Periodic heartbeats (every 5s)
- Mark nodes unhealthy after missed heartbeats
- Track node capabilities (executor types supported)
- Track node load (active tasks)

**Interface Sketch:**

```go
type NodeRegistry interface {
    Register(node NodeInfo) error
    Deregister(nodeID string) error
    Heartbeat(nodeID string, status NodeStatus) error
    GetHealthyNodes() []NodeInfo
    GetNode(nodeID string) (NodeInfo, bool)
}

type NodeInfo struct {
    ID           string
    Address      string   // host:port
    Capabilities []string // e.g., ["command", "http"]
    MaxTasks     int
    ActiveTasks  int
    LastSeen     time.Time
}
```

### 1.4 Load Balancer

**Responsibility:** Distribute tasks across healthy workers.

```
Task → Select Node → Dispatch → Track Assignment
```

**Strategies (implement at least Round Robin for MVP):**

| Strategy | Description | When to Use |
|----------|-------------|-------------|
| Round Robin | Cycle through nodes sequentially | Equal nodes, even load |
| Weighted Round Robin | Nodes get proportional share | Heterogeneous nodes |
| Least Connections | Pick node with fewest active tasks | Variable task duration |
| Random | Random selection | Simplest, good enough often |

**Interface Sketch:**

```go
type LoadBalancer interface {
    SelectNode(task Task, nodes []NodeInfo) (NodeInfo, error)
    RecordDispatch(nodeID, taskID string)
    RecordCompletion(nodeID, taskID string)
}

// Implementations
type RoundRobinBalancer struct { ... }
type WeightedBalancer struct { ... }
type LeastConnectionsBalancer struct { ... }
```

### 1.5 Dispatcher

**Responsibility:** Actually send tasks to workers and collect results.

```
Selected Node → gRPC Call → Wait for Result → Return to Scheduler
```

**Key Behaviors:**
- Maintain gRPC client pool to workers
- Implement timeouts and retries
- Handle connection failures gracefully
- Stream results for long-running tasks (optional for MVP)

### 1.6 Auto Scaler

**Responsibility:** Proactively scale worker nodes when capacity is insufficient.

```
Queue Depth Check → Capacity Evaluation → Scale Decision → K8s API Call
```

**Key Behaviors:**
- Monitor pending task queue depth
- Track worker capacity (active vs max tasks)
- Proactively scale up when no workers available or queue is backing up
- Scale down when workers are idle for extended period
- Call K8s API directly to adjust deployment replicas

**Interface Sketch:**

```go
type AutoScaler interface {
    // EnsureCapacity scales workers if needed for pending tasks
    EnsureCapacity(ctx context.Context, pendingTasks int) error
    
    // ScaleUp adds N workers
    ScaleUp(ctx context.Context, count int) error
    
    // ScaleDown removes N idle workers
    ScaleDown(ctx context.Context, count int) error
    
    // GetCurrentScale returns current and desired replica count
    GetCurrentScale(ctx context.Context) (current, desired int, err error)
}

type ScalerConfig struct {
    MinReplicas       int           // Minimum workers (e.g., 3)
    MaxReplicas       int           // Maximum workers (e.g., 20)
    ScaleUpThreshold  int           // Pending tasks per worker that triggers scale up
    ScaleDownDelay    time.Duration // Idle time before scaling down
    CooldownPeriod    time.Duration // Time between scaling operations
}
```

**Scaling Logic:**

```go
func (s *AutoScaler) EnsureCapacity(ctx context.Context, pendingTasks int) error {
    nodes := s.registry.GetHealthyNodes()
    totalCapacity := 0
    for _, n := range nodes {
        totalCapacity += n.MaxTasks - n.ActiveTasks
    }
    
    // If no capacity and tasks pending, scale up immediately
    if totalCapacity == 0 && pendingTasks > 0 {
        needed := (pendingTasks / s.cfg.TasksPerWorker) + 1
        return s.ScaleUp(ctx, min(needed, s.cfg.MaxReplicas-len(nodes)))
    }
    
    // If queue is backing up, scale up proactively
    if pendingTasks > len(nodes) * s.cfg.ScaleUpThreshold {
        return s.ScaleUp(ctx, 1)
    }
    
    return nil
}
```

---

## 2. Worker Node

Stateless execution units. Run as pods in K8s.

### 2.1 gRPC Server

**Responsibility:** Receive tasks from master.

**Key Behaviors:**
- Implement ExecutionService
- Register with master on startup
- Send heartbeats periodically (background goroutine)
- Graceful shutdown (drain in-flight tasks)

### 2.2 Task Runner

**Responsibility:** Execute received tasks via the appropriate executor.

```
Received Task → Select Executor → Execute → Return Result
```

**Key Behaviors:**
- Route tasks to correct executor based on task type
- Enforce task timeouts (context.WithTimeout)
- Capture stdout/stderr/exit code
- Resource limits (future: cgroups/namespace isolation)

**Interface Sketch:**

```go
type TaskRunner interface {
    Run(ctx context.Context, task Task) (Result, error)
    RegisterExecutor(name string, executor Executor)
    Capabilities() []string
}
```

### 2.3 Executor Plugin

**Responsibility:** Actually execute the workload. **This is the swappable part.**

See [04-EXECUTOR-PLUGIN.md](./04-EXECUTOR-PLUGIN.md) for detailed design.

---

## 3. CLI Client

Simple CLI for submitting commands and checking status.

### Commands

```bash
# Submit a command
dist-exec run "ls -la /tmp" --timeout 30s

# Check job status
dist-exec status <job-id>

# List recent jobs
dist-exec list --limit 10

# Stream logs (future)
dist-exec logs <job-id> --follow
```

### Implementation Notes

- Use cobra for CLI framework
- gRPC client to master
- Support JSON output for scripting

---

## 4. Component Interaction Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                   CLIENT                                     │
└─────────────────────────────────────┬───────────────────────────────────────┘
                                      │ Submit("ls -la")
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                              MASTER NODE                                     │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐    ┌───────────┐  │
│  │   Request    │───▶│  Scheduler   │───▶│ Load Balancer│───▶│ Dispatcher│  │
│  │   Handler    │    │              │    │              │    │           │  │
│  └──────────────┘    └──────────────┘    └───────┬──────┘    └─────┬─────┘  │
│                                                  │                  │        │
│                                          ┌───────▼──────┐           │        │
│                                          │ Node Registry│◀──────────┘        │
│                                          │              │                    │
│                                          └───────┬──────┘                    │
└──────────────────────────────────────────────────┼──────────────────────────┘
                                                   │
                     ┌─────────────────────────────┼─────────────────────────┐
                     │                             │                         │
         ┌───────────▼───────────┐     ┌───────────▼───────────┐            │
         │                       │     │                       │            │
         │  ┌─────────────────┐  │     │  ┌─────────────────┐  │            │
         │  │  gRPC Server    │  │     │  │  gRPC Server    │  │            │
         │  └────────┬────────┘  │     │  └────────┬────────┘  │            │
         │           │           │     │           │           │            │
         │  ┌────────▼────────┐  │     │  ┌────────▼────────┐  │            │
         │  │  Task Runner    │  │     │  │  Task Runner    │  │     ...    │
         │  └────────┬────────┘  │     │  └────────┬────────┘  │            │
         │           │           │     │           │           │            │
         │  ┌────────▼────────┐  │     │  ┌────────▼────────┐  │            │
         │  │ CommandExecutor │  │     │  │ CommandExecutor │  │            │
         │  └─────────────────┘  │     │  └─────────────────┘  │            │
         │                       │     │                       │            │
         │     WORKER NODE 1     │     │     WORKER NODE 2     │            │
         └───────────────────────┘     └───────────────────────┘            │
                     │                             │                         │
                     └─────────────────────────────┴─────────────────────────┘
                                          │
                                  Heartbeat (periodic)
                                          │
                                          ▼
                               Register/Deregister
```

---

## 5. State Machine: Task Lifecycle

```
     ┌──────────┐
     │  CREATED │
     └────┬─────┘
          │ Enqueue
          ▼
     ┌──────────┐
     │ PENDING  │◀────────────────────────────┐
     └────┬─────┘                             │
          │ Dispatch                          │ Retry
          ▼                                   │
     ┌──────────┐      Timeout/         ┌─────┴─────┐
     │ RUNNING  │─────────Error────────▶│  FAILED   │
     └────┬─────┘                       └───────────┘
          │ Success
          ▼
     ┌──────────┐
     │COMPLETED │
     └──────────┘
```

**States:**
- **CREATED:** Job submitted, validated
- **PENDING:** In scheduler queue, waiting for dispatch
- **RUNNING:** Dispatched to worker, executing
- **COMPLETED:** Finished successfully
- **FAILED:** Error occurred, may be retried

---

## 6. Critical Data Structures

### Task

```go
type Task struct {
    ID          string            // UUID
    Type        string            // "command", "http", etc.
    Payload     []byte            // Task-specific data (JSON)
    Priority    int               // Higher = more urgent
    Timeout     time.Duration     // Max execution time
    Retries     int               // Retry count so far
    MaxRetries  int               // Max allowed retries
    CreatedAt   time.Time
    StartedAt   *time.Time
    CompletedAt *time.Time
    Status      TaskStatus
    WorkerID    string            // Assigned worker
    Metadata    map[string]string // Arbitrary metadata
}
```

### Result

```go
type Result struct {
    TaskID     string
    Status     ResultStatus  // SUCCESS, FAILURE, TIMEOUT, CANCELLED
    Output     []byte        // stdout or response body
    Error      string        // Error message if failed
    ExitCode   int           // For command execution
    Duration   time.Duration // Actual execution time
    WorkerID   string        // Which worker executed
    CompletedAt time.Time
}
```

### NodeInfo

```go
type NodeInfo struct {
    ID           string
    Address      string        // "host:port"
    Capabilities []string      // Supported executor types
    MaxTasks     int           // Concurrent task limit
    ActiveTasks  int           // Currently running tasks
    LastSeen     time.Time     // Last heartbeat
    Status       NodeStatus    // HEALTHY, UNHEALTHY, DRAINING
    Labels       map[string]string // For affinity/anti-affinity
}
```
