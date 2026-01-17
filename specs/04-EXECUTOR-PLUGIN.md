# Executor Plugin Design

**Parent:** [00-OVERVIEW.md](./00-OVERVIEW.md)

---

## The Core Insight

The **Executor** is the swappable heart of the system. Everything else (master, load balancing, scheduling, networking) is the **distribution layer**. The Executor is the **workload layer**.

```
┌─────────────────────────────────────────────────────────────────┐
│                    TODAY                                         │
│  Distribution Layer → CommandExecutor → "ls -la" output         │
├─────────────────────────────────────────────────────────────────┤
│                    TOMORROW                                      │
│  Distribution Layer → HTTPExecutor → HTTP response              │
│  Distribution Layer → DatabaseExecutor → Query results          │
│  Distribution Layer → InferenceExecutor → ML predictions        │
└─────────────────────────────────────────────────────────────────┘
```

This document defines the plugin interface that makes this possible.

---

## 1. The Executor Interface

```go
package executor

import (
    "context"
)

// Executor defines the contract for all workload executors.
// This is the KEY interface that enables modularity.
type Executor interface {
    // Name returns the executor type name (e.g., "command", "http")
    Name() string
    
    // Execute runs the workload and returns the result.
    // - ctx includes timeout/cancellation
    // - task contains the workload payload
    // Returns result or error
    Execute(ctx context.Context, task Task) (Result, error)
    
    // Validate checks if a task payload is valid for this executor.
    // Called before enqueueing to fail fast.
    Validate(payload []byte) error
    
    // HealthCheck verifies the executor can operate.
    // Called during worker health checks.
    HealthCheck(ctx context.Context) error
}

// Task represents work to be executed.
type Task struct {
    ID       string
    Type     string            // Matches Executor.Name()
    Payload  []byte            // Executor-specific data
    Metadata map[string]string // Optional context
}

// Result represents execution outcome.
type Result struct {
    Output    []byte            // Executor-specific output
    ExitCode  int               // Meaningful for command, 0 for others
    Error     string            // Error message if any
    Metadata  map[string]string // Executor-specific metadata
}
```

---

## 2. Command Executor (MVP Implementation)

```go
package executor

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
)

// CommandPayload is the input format for command execution.
type CommandPayload struct {
    Command    string            `json:"command"`
    Args       []string          `json:"args,omitempty"`
    Env        map[string]string `json:"env,omitempty"`
    WorkingDir string            `json:"working_dir,omitempty"`
    Stdin      string            `json:"stdin,omitempty"`
}

// CommandExecutor executes shell commands.
type CommandExecutor struct {
    // Optional: allowed commands whitelist for security
    AllowedCommands []string
    
    // Optional: blocked commands blacklist  
    BlockedCommands []string
}

func NewCommandExecutor() *CommandExecutor {
    return &CommandExecutor{}
}

func (e *CommandExecutor) Name() string {
    return "command"
}

func (e *CommandExecutor) Validate(payload []byte) error {
    var p CommandPayload
    if err := json.Unmarshal(payload, &p); err != nil {
        return fmt.Errorf("invalid payload: %w", err)
    }
    if p.Command == "" {
        return fmt.Errorf("command is required")
    }
    // Could add security validation here
    return nil
}

func (e *CommandExecutor) Execute(ctx context.Context, task Task) (Result, error) {
    var payload CommandPayload
    if err := json.Unmarshal(task.Payload, &payload); err != nil {
        return Result{}, fmt.Errorf("payload parse error: %w", err)
    }
    
    // Build the command
    cmd := exec.CommandContext(ctx, payload.Command, payload.Args...)
    
    // Set working directory
    if payload.WorkingDir != "" {
        cmd.Dir = payload.WorkingDir
    }
    
    // Set environment
    if len(payload.Env) > 0 {
        env := os.Environ()
        for k, v := range payload.Env {
            env = append(env, k+"="+v)
        }
        cmd.Env = env
    }
    
    // Set stdin
    if payload.Stdin != "" {
        cmd.Stdin = strings.NewReader(payload.Stdin)
    }
    
    // Capture output
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    
    // Execute
    err := cmd.Run()
    
    // Build result
    result := Result{
        Output: stdout.Bytes(),
        Metadata: map[string]string{
            "stderr": stderr.String(),
        },
    }
    
    if err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok {
            result.ExitCode = exitErr.ExitCode()
        } else {
            result.Error = err.Error()
            result.ExitCode = -1
        }
    }
    
    return result, nil
}

func (e *CommandExecutor) HealthCheck(ctx context.Context) error {
    // Simple check: can we run 'true' command?
    cmd := exec.CommandContext(ctx, "true")
    return cmd.Run()
}
```

---

## 3. Future Executor Examples

### 3.1 HTTP Executor

For distributed HTTP request execution (API testing, health checks, etc.):

```go
type HTTPPayload struct {
    Method  string            `json:"method"`
    URL     string            `json:"url"`
    Headers map[string]string `json:"headers,omitempty"`
    Body    string            `json:"body,omitempty"`
    Timeout int               `json:"timeout_seconds,omitempty"`
}

type HTTPExecutor struct {
    client *http.Client
}

func (e *HTTPExecutor) Name() string {
    return "http"
}

func (e *HTTPExecutor) Execute(ctx context.Context, task Task) (Result, error) {
    var payload HTTPPayload
    json.Unmarshal(task.Payload, &payload)
    
    req, _ := http.NewRequestWithContext(ctx, payload.Method, payload.URL, 
        strings.NewReader(payload.Body))
    
    for k, v := range payload.Headers {
        req.Header.Set(k, v)
    }
    
    resp, err := e.client.Do(req)
    if err != nil {
        return Result{Error: err.Error()}, nil
    }
    defer resp.Body.Close()
    
    body, _ := io.ReadAll(resp.Body)
    
    return Result{
        Output:   body,
        ExitCode: resp.StatusCode,
        Metadata: map[string]string{
            "status": resp.Status,
        },
    }, nil
}
```

### 3.2 Database Executor

For distributed query execution:

```go
type DatabasePayload struct {
    DSN   string `json:"dsn"`
    Query string `json:"query"`
}

type DatabaseExecutor struct {
    pools sync.Map // Connection pool cache
}

func (e *DatabaseExecutor) Name() string {
    return "database"
}

// Implementation would execute SQL and return results as JSON
```

### 3.3 Container Executor

For running tasks in isolated containers:

```go
type ContainerPayload struct {
    Image   string            `json:"image"`
    Command []string          `json:"command"`
    Env     map[string]string `json:"env,omitempty"`
    Mounts  []Mount           `json:"mounts,omitempty"`
}

type ContainerExecutor struct {
    client *docker.Client
}

func (e *ContainerExecutor) Name() string {
    return "container"
}

// Implementation would use Docker API to run containers
```

---

## 4. Executor Registry

Workers maintain a registry of available executors:

```go
package executor

import (
    "fmt"
    "sync"
)

// Registry manages executor instances.
type Registry struct {
    mu        sync.RWMutex
    executors map[string]Executor
}

func NewRegistry() *Registry {
    return &Registry{
        executors: make(map[string]Executor),
    }
}

// Register adds an executor.
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
```

---

## 5. Worker Integration

How the worker uses the executor registry:

```go
package worker

type Worker struct {
    id       string
    registry *executor.Registry
    // ... other fields
}

func NewWorker(cfg Config) *Worker {
    w := &Worker{
        id:       cfg.ID,
        registry: executor.NewRegistry(),
    }
    
    // Register executors based on configuration
    w.registry.Register(executor.NewCommandExecutor())
    
    // Future: conditional registration
    // if cfg.EnableHTTP {
    //     w.registry.Register(executor.NewHTTPExecutor(client))
    // }
    
    return w
}

// Execute handles the gRPC Execute call
func (w *Worker) Execute(ctx context.Context, req *pb.ExecuteRequest) (*pb.ExecuteResponse, error) {
    task := executor.Task{
        ID:       req.TaskId,
        Type:     req.TaskType,
        Payload:  req.Payload,
        Metadata: req.Metadata,
    }
    
    // Apply timeout
    if req.Timeout != nil {
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, req.Timeout.AsDuration())
        defer cancel()
    }
    
    result, err := w.registry.Execute(ctx, task)
    if err != nil {
        return nil, status.Errorf(codes.Internal, "execution failed: %v", err)
    }
    
    return &pb.ExecuteResponse{
        TaskId:   req.TaskId,
        Status:   statusFromResult(result),
        Output:   result.Output,
        Error:    result.Error,
        ExitCode: int32(result.ExitCode),
    }, nil
}
```

---

## 6. Configuration-Driven Registration

For flexibility, load executors from configuration:

```yaml
# config.yaml
executors:
  command:
    enabled: true
    allowed_commands: ["ls", "cat", "grep", "head", "tail"]
    blocked_commands: ["rm", "sudo", "dd"]
  http:
    enabled: true
    timeout_seconds: 30
    max_body_size: 1048576
  container:
    enabled: false  # Disabled for MVP
```

```go
func LoadExecutors(cfg Config, registry *executor.Registry) error {
    if cfg.Executors.Command.Enabled {
        cmdExec := executor.NewCommandExecutor()
        cmdExec.AllowedCommands = cfg.Executors.Command.AllowedCommands
        cmdExec.BlockedCommands = cfg.Executors.Command.BlockedCommands
        registry.Register(cmdExec)
    }
    
    if cfg.Executors.HTTP.Enabled {
        httpExec := executor.NewHTTPExecutor(executor.HTTPConfig{
            Timeout:     cfg.Executors.HTTP.TimeoutSeconds,
            MaxBodySize: cfg.Executors.HTTP.MaxBodySize,
        })
        registry.Register(httpExec)
    }
    
    return nil
}
```

---

## 7. Testing Executors

Each executor should be thoroughly tested in isolation:

```go
func TestCommandExecutor_Execute(t *testing.T) {
    e := NewCommandExecutor()
    ctx := context.Background()
    
    tests := []struct {
        name     string
        payload  CommandPayload
        wantCode int
        wantOut  string
    }{
        {
            name:     "simple echo",
            payload:  CommandPayload{Command: "echo", Args: []string{"hello"}},
            wantCode: 0,
            wantOut:  "hello\n",
        },
        {
            name:     "nonexistent command",
            payload:  CommandPayload{Command: "nonexistent_cmd_xyz"},
            wantCode: -1,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            payloadBytes, _ := json.Marshal(tt.payload)
            task := Task{ID: "test", Type: "command", Payload: payloadBytes}
            
            result, _ := e.Execute(ctx, task)
            
            if result.ExitCode != tt.wantCode {
                t.Errorf("got exit code %d, want %d", result.ExitCode, tt.wantCode)
            }
            if tt.wantOut != "" && string(result.Output) != tt.wantOut {
                t.Errorf("got output %q, want %q", result.Output, tt.wantOut)
            }
        })
    }
}
```

---

## 8. Design Principles

### Principle 1: Accept Interfaces, Return Structs

The `Executor` interface allows diverse implementations. The `Result` struct is concrete and well-defined.

### Principle 2: No External State

Executors should be stateless. State (connections, caches) can be held internally but execution should be idempotent.

### Principle 3: Context-Aware

Always respect `ctx` for cancellation and timeouts. Long-running executors must check `ctx.Done()`.

### Principle 4: Fail Fast

The `Validate` method catches errors before tasks enter the queue. Better to reject immediately than fail during execution.

### Principle 5: Rich Metadata

Return useful metadata (stderr, headers, timing) - consumers may need it for debugging.

---

## Research for Implementers

- **os/exec package:** https://pkg.go.dev/os/exec
- **Context patterns:** https://go.dev/blog/context
- **Plugin pattern in Go:** Look at HashiCorp's go-plugin (though overkill for MVP)
- **Executor isolation:** Consider namespaces/cgroups for production (gvisor, nsjail)
