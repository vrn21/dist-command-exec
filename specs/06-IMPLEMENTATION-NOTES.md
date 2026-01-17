# Implementation Notes & Research Links

**Parent:** [00-OVERVIEW.md](./00-OVERVIEW.md)

---

## Purpose

This document is for implementing agents. It contains research links, OSS references, implementation hints, and common pitfalls to avoid.

---

## 1. OSS Projects to Study

### Core Patterns

| Project | What to Learn | Key Files |
|---------|--------------|-----------|
| **[etcd](https://github.com/etcd-io/etcd)** | gRPC service design, client libraries | `api/`, `client/v3/` |
| **[CockroachDB](https://github.com/cockroachdb/cockroach)** | Distributed architecture, protobuf layout | `pkg/server/`, `pkg/rpc/` |
| **[HashiCorp Nomad](https://github.com/hashicorp/nomad)** | Job scheduling, executor model | `nomad/structs/`, `drivers/` |
| **[Vitess](https://github.com/vitessio/vitess)** | gRPC patterns, worker pools | `go/vt/` |
| **[Temporal](https://github.com/temporalio/temporal)** | Workflow orchestration, worker design | `service/`, `client/` |

### Specific Patterns

| Pattern | Reference |
|---------|-----------|
| Round robin load balancer | [gRPC-Go round_robin.go](https://github.com/grpc/grpc-go/blob/master/balancer/roundrobin/roundrobin.go) |
| Health checking | [grpc-health-probe](https://github.com/grpc-ecosystem/grpc-health-probe) |
| Graceful shutdown | [Kubernetes lifecycle](https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/) |
| Context patterns | [Go Blog: Context](https://go.dev/blog/context) |
| Error handling | [Go Blog: Working with Errors](https://go.dev/blog/go1.13-errors) |

---

## 2. gRPC Implementation Tips

### Connection Pooling

The master maintains connections to workers. Use connection pooling:

```go
// Recommended: connection per worker, reuse across calls
type WorkerPool struct {
    mu    sync.RWMutex
    conns map[string]*grpc.ClientConn
}

func (p *WorkerPool) Get(addr string) (*grpc.ClientConn, error) {
    p.mu.RLock()
    conn, ok := p.conns[addr]
    p.mu.RUnlock()
    if ok {
        return conn, nil
    }

    p.mu.Lock()
    defer p.mu.Unlock()
    
    // Double-check
    if conn, ok := p.conns[addr]; ok {
        return conn, nil
    }

    conn, err := grpc.Dial(addr, 
        grpc.WithTransportCredentials(insecure.NewCredentials()),
        grpc.WithKeepaliveParams(keepalive.ClientParameters{
            Time:    10 * time.Second,
            Timeout: 3 * time.Second,
        }),
    )
    if err != nil {
        return nil, err
    }

    p.conns[addr] = conn
    return conn, nil
}
```

### Streaming Best Practices

```go
// Server streaming - send chunks as they become available
func (s *Server) StreamOutput(
    req *pb.StreamOutputRequest,
    stream pb.MasterService_StreamOutputServer,
) error {
    ch := s.subscribeToJob(req.JobId)
    defer s.unsubscribe(ch)

    for {
        select {
        case <-stream.Context().Done():
            return nil
        case chunk, ok := <-ch:
            if !ok {
                return nil // Job completed
            }
            if err := stream.Send(&pb.OutputChunk{Data: chunk}); err != nil {
                return err
            }
        }
    }
}
```

### Error Handling

Always use gRPC status codes properly:

```go
import (
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

// Good - specific error codes
return nil, status.Errorf(codes.NotFound, "job %s not found", jobID)
return nil, status.Errorf(codes.InvalidArgument, "command is required")
return nil, status.Errorf(codes.ResourceExhausted, "queue is full")
return nil, status.Errorf(codes.Unavailable, "no healthy workers")

// Bad - generic error
return nil, err // Becomes codes.Unknown
```

---

## 3. Concurrency Patterns

### Task Queue with Bounded Workers

```go
type TaskRunner struct {
    sem    chan struct{}       // Semaphore for concurrency limit
    active sync.WaitGroup
}

func NewTaskRunner(maxConcurrent int) *TaskRunner {
    return &TaskRunner{
        sem: make(chan struct{}, maxConcurrent),
    }
}

func (r *TaskRunner) Run(ctx context.Context, task Task, exec Executor) {
    // Acquire slot
    select {
    case r.sem <- struct{}{}:
        // Got slot
    case <-ctx.Done():
        return
    }

    r.active.Add(1)
    go func() {
        defer r.active.Done()
        defer func() { <-r.sem }() // Release slot

        result, err := exec.Execute(ctx, task)
        // Handle result...
    }()
}

func (r *TaskRunner) WaitForDrain(ctx context.Context) {
    done := make(chan struct{})
    go func() {
        r.active.Wait()
        close(done)
    }()

    select {
    case <-done:
    case <-ctx.Done():
    }
}
```

### Safe Map Access

```go
// Use sync.Map for high-read contention
var nodes sync.Map // map[string]*NodeInfo

// Or use RWMutex for complex operations
type Registry struct {
    mu    sync.RWMutex
    nodes map[string]*NodeInfo
}

func (r *Registry) Get(id string) (*NodeInfo, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    n, ok := r.nodes[id]
    return n, ok
}

func (r *Registry) Update(id string, fn func(*NodeInfo)) {
    r.mu.Lock()
    defer r.mu.Unlock()
    if n, ok := r.nodes[id]; ok {
        fn(n)
    }
}
```

---

## 4. Testing Strategies

### Unit Tests

Test components in isolation:

```go
func TestScheduler_Enqueue(t *testing.T) {
    s := NewScheduler()
    task := types.Task{ID: "test-1", Type: "command"}

    err := s.Enqueue(task)
    require.NoError(t, err)

    got, ok := s.GetTask("test-1")
    require.True(t, ok)
    assert.Equal(t, "test-1", got.ID)
}
```

### Integration Tests with Docker

```go
// +build integration

func TestMasterWorkerIntegration(t *testing.T) {
    // Start master
    masterCfg := config.Master{Address: ":50051"}
    master := master.NewServer(masterCfg)
    go master.Run(context.Background())

    // Start worker
    workerCfg := config.Worker{
        ID:            "test-worker",
        MasterAddress: "localhost:50051",
    }
    worker := worker.NewServer(workerCfg)
    go worker.Run(context.Background())

    // Wait for registration
    time.Sleep(time.Second)

    // Submit a job
    client, _ := client.New("localhost:50051")
    result, err := client.RunCommand(context.Background(), "echo", "hello")
    
    require.NoError(t, err)
    assert.Contains(t, result, "hello")
}
```

### Mocking gRPC

```go
// Mock for testing
type MockWorkerClient struct {
    ExecuteFunc func(ctx context.Context, req *pb.ExecuteRequest) (*pb.ExecuteResponse, error)
}

func (m *MockWorkerClient) Execute(ctx context.Context, req *pb.ExecuteRequest, opts ...grpc.CallOption) (*pb.ExecuteResponse, error) {
    return m.ExecuteFunc(ctx, req)
}

func TestDispatcher_DispatchWithMock(t *testing.T) {
    mock := &MockWorkerClient{
        ExecuteFunc: func(ctx context.Context, req *pb.ExecuteRequest) (*pb.ExecuteResponse, error) {
            return &pb.ExecuteResponse{
                TaskId:   req.TaskId,
                Status:   pb.ExecutionStatus_EXECUTION_STATUS_SUCCESS,
                Output:   []byte("test output"),
                ExitCode: 0,
            }, nil
        },
    }

    d := NewDispatcher(mock)
    result, err := d.Dispatch(context.Background(), task)
    
    require.NoError(t, err)
    assert.Equal(t, "test output", string(result.Output))
}
```

---

## 5. Common Pitfalls

### Pitfall 1: Not Respecting Context

```go
// BAD - ignores cancellation
func (e *Executor) Execute(ctx context.Context, task Task) (Result, error) {
    // Long running operation without context check
    time.Sleep(10 * time.Second)
    return Result{}, nil
}

// GOOD - respects cancellation
func (e *Executor) Execute(ctx context.Context, task Task) (Result, error) {
    select {
    case <-ctx.Done():
        return Result{}, ctx.Err()
    case <-time.After(10 * time.Second):
        return Result{}, nil
    }
}
```

### Pitfall 2: Race Conditions in Registry

```go
// BAD - time-of-check to time-of-use bug
nodes := registry.GetHealthyNodes()
if len(nodes) > 0 {
    // Nodes might be unhealthy by now!
    dispatch(nodes[0], task)
}

// BETTER - atomic selection or retry logic
node, err := registry.SelectAndReserve(task)
if err != nil {
    return err
}
defer registry.Release(node)
dispatch(node, task)
```

### Pitfall 3: Goroutine Leaks

```go
// BAD - goroutine leak if context cancelled
go func() {
    for task := range tasks {
        process(task)
    }
}()

// GOOD - respects context
go func() {
    for {
        select {
        case <-ctx.Done():
            return
        case task, ok := <-tasks:
            if !ok {
                return
            }
            process(task)
        }
    }
}()
```

### Pitfall 4: Blocking on Send

```go
// BAD - might block forever
ch <- task

// GOOD - bounded with context
select {
case ch <- task:
    // sent
case <-ctx.Done():
    return ctx.Err()
case <-time.After(5 * time.Second):
    return ErrTimeout
}
```

---

## 6. Implementation Order

Recommended order for building:

### Phase 1: Core Infrastructure

1. **Proto definitions** - Define all message types and services
2. **pkg/executor** - Build the executor interface and command executor
3. **pkg/types** - Shared types
4. Generate protobuf code

### Phase 2: Worker

5. **internal/worker/server.go** - Basic gRPC server
6. **internal/worker/runner.go** - Task execution
7. **internal/worker/registration.go** - Register with master
8. **cmd/worker/main.go** - Wire it together
9. Test worker in isolation (mock master)

### Phase 3: Master

10. **internal/master/registry.go** - Node registry
11. **internal/master/scheduler.go** - Task queue
12. **internal/master/balancer.go** - Load balancing
13. **internal/master/dispatcher.go** - Send tasks to workers
14. **internal/master/server.go** - gRPC endpoints
15. **cmd/master/main.go** - Wire it together

### Phase 4: CLI & Integration

16. **cmd/distctl** - CLI client
17. **docker-compose.yaml** - Local cluster
18. Integration tests

### Phase 5: K8s Deployment

19. **deploy/kubernetes/** - Manifests
20. HPA configuration
21. End-to-end testing

---

## 7. Debugging Tips

### Enable gRPC Logging

```bash
export GRPC_GO_LOG_VERBOSITY_LEVEL=99
export GRPC_GO_LOG_SEVERITY_LEVEL=info
```

### Structured Logging

```go
import "github.com/rs/zerolog/log"

log.Info().
    Str("worker_id", workerID).
    Str("task_id", task.ID).
    Dur("duration", duration).
    Msg("task completed")
```

### Health Check Debugging

```bash
# Install grpc-health-probe
go install github.com/grpc-ecosystem/grpc-health-probe@latest

# Check health
grpc-health-probe -addr=localhost:50051
```

### gRPCurl for Testing

```bash
# Install
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest

# List services
grpcurl -plaintext localhost:50051 list

# Call a method
grpcurl -plaintext -d '{"task_type": "command", "payload": "..."}' \
    localhost:50051 distexec.v1.MasterService/Submit
```

---

## 8. Quick Reference

### Key Go Packages

```go
import (
    // gRPC
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "google.golang.org/grpc/health"
    "google.golang.org/grpc/health/grpc_health_v1"
    
    // Protobuf
    "google.golang.org/protobuf/proto"
    "google.golang.org/protobuf/types/known/timestamppb"
    "google.golang.org/protobuf/types/known/durationpb"
    
    // Standard
    "context"
    "sync"
    "os/exec"
    "encoding/json"
)
```

### Environment Variables

| Variable | Component | Default | Description |
|----------|-----------|---------|-------------|
| MASTER_ADDRESS | Both | :50051 | Master gRPC address |
| WORKER_ID | Worker | (required) | Unique worker identifier |
| WORKER_ADDRESS | Worker | :50052 | Worker gRPC listen address |
| MAX_CONCURRENT_TASKS | Worker | 10 | Max parallel executions |
| HEARTBEAT_INTERVAL_SECONDS | Worker | 5 | Heartbeat frequency |
| LOG_LEVEL | Both | info | debug/info/warn/error |

---

## 9. Checklist Before Submission

- [ ] All protos compile with `buf generate`
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes
- [ ] No data races (`go test -race ./...`)
- [ ] `golangci-lint run` clean
- [ ] Docker images build
- [ ] docker-compose up starts cluster
- [ ] Manual test: submit job via CLI, get result
- [ ] Graceful shutdown works (SIGTERM)
- [ ] Worker reconnects if master restarts
