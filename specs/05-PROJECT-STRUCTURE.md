# Project Structure & Go Package Design

**Parent:** [00-OVERVIEW.md](./00-OVERVIEW.md)

---

## Overview

This document defines the idiomatic Go project structure for the distributed command execution system. The layout follows standard Go conventions and keeps the codebase clean and maintainable.

---

## 1. Directory Layout

```
dist-command-exec/
├── README.md
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile.master
├── Dockerfile.worker
│
├── specs/                      # Architecture documents (you are here)
│   ├── 00-OVERVIEW.md
│   ├── 01-COMPONENTS.md
│   ├── 02-RPC-PROTOCOL.md
│   ├── 03-DYNAMIC-SCALING.md
│   ├── 04-EXECUTOR-PLUGIN.md
│   ├── 05-PROJECT-STRUCTURE.md
│   └── 06-IMPLEMENTATION-NOTES.md
│
├── proto/                      # Protocol buffer definitions
│   ├── buf.yaml
│   ├── buf.gen.yaml
│   └── distexec/
│       └── v1/
│           ├── master.proto
│           ├── worker.proto
│           ├── registration.proto
│           └── common.proto
│
├── gen/                        # Generated code (gitignore optional)
│   └── go/
│       └── distexec/
│           └── v1/
│               ├── master.pb.go
│               ├── master_grpc.pb.go
│               ├── worker.pb.go
│               ├── worker_grpc.pb.go
│               ├── registration.pb.go
│               ├── registration_grpc.pb.go
│               └── common.pb.go
│
├── cmd/                        # Entry points (main packages)
│   ├── master/
│   │   └── main.go             # Master node binary
│   ├── worker/
│   │   └── main.go             # Worker node binary
│   └── distctl/
│       └── main.go             # CLI client
│
├── internal/                   # Private application code
│   ├── master/
│   │   ├── server.go           # gRPC server implementation
│   │   ├── scheduler.go        # Task scheduling
│   │   ├── registry.go         # Node registry
│   │   ├── balancer.go         # Load balancing
│   │   ├── dispatcher.go       # Task dispatch
│   │   ├── scaler.go           # Auto-scaling via K8s API
│   │   └── store.go            # Job state storage
│   │
│   ├── worker/
│   │   ├── server.go           # gRPC server implementation
│   │   ├── runner.go           # Task runner
│   │   └── registration.go     # Master registration logic
│   │
│   └── config/
│       ├── config.go           # Configuration loading
│       ├── master.go           # Master-specific config
│       └── worker.go           # Worker-specific config
│
├── pkg/                        # Public packages (can be imported)
│   ├── executor/               # The swappable executor interface
│   │   ├── executor.go         # Core interface
│   │   ├── command.go          # Command executor
│   │   ├── registry.go         # Executor registry
│   │   └── command_test.go     # Tests
│   │
│   ├── types/                  # Shared types
│   │   ├── task.go
│   │   └── result.go
│   │
│   └── client/                 # Client library for programmatic use
│       └── client.go           # Master client wrapper
│
├── deploy/                     # Deployment configurations
│   └── kubernetes/
│       ├── master-deployment.yaml
│       ├── master-service.yaml
│       ├── master-rbac.yaml        # RBAC for K8s API scaling
│       ├── worker-deployment.yaml
│       ├── worker-service.yaml
│       └── worker-hpa.yaml         # Optional: HPA as backup
│
├── scripts/                    # Development scripts
│   ├── proto-gen.sh
│   └── local-cluster.sh
│
└── docker-compose.yaml         # Local development setup
```

---

## 2. Package Responsibilities

### cmd/master/main.go

Entry point for the master node. Minimal logic - just wiring.

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"

    "dist-command-exec/internal/config"
    "dist-command-exec/internal/master"
)

func main() {
    cfg, err := config.LoadMaster()
    if err != nil {
        log.Fatalf("config error: %v", err)
    }

    srv, err := master.NewServer(cfg)
    if err != nil {
        log.Fatalf("server creation failed: %v", err)
    }

    // Graceful shutdown
    ctx, cancel := signal.NotifyContext(context.Background(),
        syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    if err := srv.Run(ctx); err != nil {
        log.Fatalf("server error: %v", err)
    }
}
```

### cmd/worker/main.go

Entry point for worker nodes. Similar pattern.

```go
package main

import (
    "context"
    "log"
    "os/signal"
    "syscall"

    "dist-command-exec/internal/config"
    "dist-command-exec/internal/worker"
    "dist-command-exec/pkg/executor"
)

func main() {
    cfg, err := config.LoadWorker()
    if err != nil {
        log.Fatalf("config error: %v", err)
    }

    // Build executor registry
    registry := executor.NewRegistry()
    registry.Register(executor.NewCommandExecutor())

    srv, err := worker.NewServer(cfg, registry)
    if err != nil {
        log.Fatalf("server creation failed: %v", err)
    }

    ctx, cancel := signal.NotifyContext(context.Background(),
        syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    if err := srv.Run(ctx); err != nil {
        log.Fatalf("server error: %v", err)
    }
}
```

### cmd/distctl/main.go

CLI using cobra:

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
)

func main() {
    rootCmd := &cobra.Command{
        Use:   "distctl",
        Short: "CLI for distributed command execution",
    }

    rootCmd.AddCommand(
        runCmd(),
        statusCmd(),
        listCmd(),
    )

    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

---

## 3. Internal Packages

### internal/master/server.go

```go
package master

import (
    "context"
    "net"

    "google.golang.org/grpc"
    "google.golang.org/grpc/health"
    "google.golang.org/grpc/health/grpc_health_v1"

    pb "dist-command-exec/gen/go/distexec/v1"
)

type Server struct {
    cfg        Config
    grpcServer *grpc.Server
    scheduler  *Scheduler
    registry   *NodeRegistry
    balancer   LoadBalancer
    dispatcher *Dispatcher

    pb.UnimplementedMasterServiceServer
    pb.UnimplementedRegistrationServiceServer
}

func NewServer(cfg Config) (*Server, error) {
    s := &Server{
        cfg:       cfg,
        scheduler: NewScheduler(),
        registry:  NewNodeRegistry(),
        balancer:  NewRoundRobinBalancer(),
    }
    s.dispatcher = NewDispatcher(s.registry)

    // Setup gRPC
    s.grpcServer = grpc.NewServer()
    pb.RegisterMasterServiceServer(s.grpcServer, s)
    pb.RegisterRegistrationServiceServer(s.grpcServer, s)

    // Health service
    healthSrv := health.NewServer()
    grpc_health_v1.RegisterHealthServer(s.grpcServer, healthSrv)
    healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

    return s, nil
}

func (s *Server) Run(ctx context.Context) error {
    lis, err := net.Listen("tcp", s.cfg.Address)
    if err != nil {
        return err
    }

    // Start background workers
    go s.scheduler.Run(ctx, s.dispatchLoop)
    go s.registry.MonitorHealth(ctx)

    // Handle shutdown
    go func() {
        <-ctx.Done()
        s.grpcServer.GracefulStop()
    }()

    return s.grpcServer.Serve(lis)
}
```

### internal/master/scheduler.go

```go
package master

import (
    "context"
    "sync"
    "time"

    "dist-command-exec/pkg/types"
)

type Scheduler struct {
    mu       sync.RWMutex
    pending  chan types.Task
    tasks    map[string]*types.Task // All tasks by ID
    maxQueue int
}

func NewScheduler() *Scheduler {
    return &Scheduler{
        pending:  make(chan types.Task, 1000),
        tasks:    make(map[string]*types.Task),
        maxQueue: 10000,
    }
}

func (s *Scheduler) Enqueue(task types.Task) error {
    s.mu.Lock()
    s.tasks[task.ID] = &task
    s.mu.Unlock()

    select {
    case s.pending <- task:
        return nil
    default:
        return ErrQueueFull
    }
}

func (s *Scheduler) Run(ctx context.Context, dispatch func(types.Task)) {
    for {
        select {
        case <-ctx.Done():
            return
        case task := <-s.pending:
            dispatch(task)
        }
    }
}

func (s *Scheduler) GetTask(id string) (*types.Task, bool) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    t, ok := s.tasks[id]
    return t, ok
}

func (s *Scheduler) UpdateStatus(id string, status types.TaskStatus) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if t, ok := s.tasks[id]; ok {
        t.Status = status
    }
}
```

### internal/master/balancer.go

```go
package master

import (
    "sync"
    "sync/atomic"

    "dist-command-exec/pkg/types"
)

type LoadBalancer interface {
    Select(task types.Task, nodes []NodeInfo) (NodeInfo, error)
}

// RoundRobinBalancer cycles through available nodes.
type RoundRobinBalancer struct {
    counter uint64
}

func NewRoundRobinBalancer() *RoundRobinBalancer {
    return &RoundRobinBalancer{}
}

func (b *RoundRobinBalancer) Select(task types.Task, nodes []NodeInfo) (NodeInfo, error) {
    if len(nodes) == 0 {
        return NodeInfo{}, ErrNoHealthyNodes
    }

    idx := atomic.AddUint64(&b.counter, 1) % uint64(len(nodes))
    return nodes[idx], nil
}

// LeastConnectionsBalancer picks the node with fewest active tasks.
type LeastConnectionsBalancer struct {
    mu    sync.RWMutex
    loads map[string]int
}

func NewLeastConnectionsBalancer() *LeastConnectionsBalancer {
    return &LeastConnectionsBalancer{
        loads: make(map[string]int),
    }
}

func (b *LeastConnectionsBalancer) Select(task types.Task, nodes []NodeInfo) (NodeInfo, error) {
    if len(nodes) == 0 {
        return NodeInfo{}, ErrNoHealthyNodes
    }

    var best NodeInfo
    minLoad := int(^uint(0) >> 1) // Max int

    b.mu.RLock()
    for _, n := range nodes {
        load := b.loads[n.ID]
        if load < minLoad {
            minLoad = load
            best = n
        }
    }
    b.mu.RUnlock()

    return best, nil
}
```

---

## 4. Public Packages (pkg/)

These can be imported by external projects.

### pkg/executor/executor.go

```go
// Package executor defines the interface for task execution.
// This package is public and can be imported to build custom executors.
package executor

import "context"

// Executor defines the contract for workload execution.
type Executor interface {
    Name() string
    Execute(ctx context.Context, task Task) (Result, error)
    Validate(payload []byte) error
    HealthCheck(ctx context.Context) error
}

// Task represents a unit of work.
type Task struct {
    ID       string
    Type     string
    Payload  []byte
    Metadata map[string]string
}

// Result is the outcome of task execution.
type Result struct {
    Output   []byte
    ExitCode int
    Error    string
    Metadata map[string]string
}
```

### pkg/client/client.go

A clean client for programmatic access:

```go
// Package client provides a Go client for the master service.
package client

import (
    "context"
    "time"

    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"

    pb "dist-command-exec/gen/go/distexec/v1"
)

type Client struct {
    conn   *grpc.ClientConn
    master pb.MasterServiceClient
}

func New(address string) (*Client, error) {
    conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        return nil, err
    }

    return &Client{
        conn:   conn,
        master: pb.NewMasterServiceClient(conn),
    }, nil
}

func (c *Client) Close() error {
    return c.conn.Close()
}

func (c *Client) RunCommand(ctx context.Context, cmd string, args ...string) (string, error) {
    payload := CommandPayload{
        Command: cmd,
        Args:    args,
    }
    payloadBytes, _ := json.Marshal(payload)

    resp, err := c.master.Submit(ctx, &pb.SubmitRequest{
        TaskType: "command",
        Payload:  payloadBytes,
        Timeout:  durationpb.New(30 * time.Second),
    })
    if err != nil {
        return "", err
    }

    // Poll for result
    return c.waitForResult(ctx, resp.JobId)
}
```

---

## 5. Configuration

### internal/config/config.go

```go
package config

import (
    "github.com/kelseyhightower/envconfig"
)

type Master struct {
    Address           string `envconfig:"MASTER_ADDRESS" default:":50051"`
    MaxConcurrentJobs int    `envconfig:"MAX_CONCURRENT_JOBS" default:"1000"`
    HeartbeatTimeout  int    `envconfig:"HEARTBEAT_TIMEOUT_SECONDS" default:"15"`
    LogLevel          string `envconfig:"LOG_LEVEL" default:"info"`
}

type Worker struct {
    ID                 string `envconfig:"WORKER_ID" required:"true"`
    Address            string `envconfig:"WORKER_ADDRESS" default:":50052"`
    MasterAddress      string `envconfig:"MASTER_ADDRESS" required:"true"`
    MaxConcurrentTasks int    `envconfig:"MAX_CONCURRENT_TASKS" default:"10"`
    HeartbeatInterval  int    `envconfig:"HEARTBEAT_INTERVAL_SECONDS" default:"5"`
    LogLevel           string `envconfig:"LOG_LEVEL" default:"info"`
}

func LoadMaster() (Master, error) {
    var cfg Master
    err := envconfig.Process("", &cfg)
    return cfg, err
}

func LoadWorker() (Worker, error) {
    var cfg Worker
    err := envconfig.Process("", &cfg)
    return cfg, err
}
```

---

## 6. Makefile

```makefile
.PHONY: all build test proto clean docker

all: proto build

# Generate protobuf code
proto:
	buf generate

# Build binaries
build:
	go build -o bin/master ./cmd/master
	go build -o bin/worker ./cmd/worker
	go build -o bin/distctl ./cmd/distctl

# Run tests
test:
	go test -v -race ./...

# Run with coverage
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

# Lint
lint:
	golangci-lint run

# Clean build artifacts
clean:
	rm -rf bin/ gen/

# Docker images
docker:
	docker build -f Dockerfile.master -t dist-exec-master:latest .
	docker build -f Dockerfile.worker -t dist-exec-worker:latest .

# Run local cluster
local-up:
	docker-compose up --build

local-down:
	docker-compose down
```

---

## 7. Dependencies (go.mod)

```go
module dist-command-exec

go 1.25.6

require (
    github.com/kelseyhightower/envconfig v1.4.0
    github.com/rs/zerolog v1.31.0
    github.com/spf13/cobra v1.8.0
    google.golang.org/grpc v1.60.0
    google.golang.org/protobuf v1.32.0
    k8s.io/api v0.29.0
    k8s.io/apimachinery v0.29.0
    k8s.io/client-go v0.29.0
)
```

---

## 8. Idiomatic Go Patterns to Follow

| Pattern | Example | Why |
|---------|---------|-----|
| Accept interfaces | `func New(balancer LoadBalancer)` | Testability, flexibility |
| Return structs | `func New() *Server` | Concrete types are clearer |
| Options pattern | `func New(opts ...Option)` | Extensible without breaking API |
| Context first | `func (s *Server) Run(ctx context.Context)` | Standard Go convention |
| Error wrapping | `fmt.Errorf("submit: %w", err)` | Error chain for debugging |
| No globals | Inject dependencies | Testable, explicit |

---

## Research for Implementers

- **Standard Go project layout:** https://github.com/golang-standards/project-layout
- **envconfig:** https://github.com/kelseyhightower/envconfig
- **Cobra CLI:** https://github.com/spf13/cobra
- **zerolog:** https://github.com/rs/zerolog
- **gRPC health:** https://pkg.go.dev/google.golang.org/grpc/health
