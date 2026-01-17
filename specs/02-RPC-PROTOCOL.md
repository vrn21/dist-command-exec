# RPC Protocol Design

**Parent:** [00-OVERVIEW.md](./00-OVERVIEW.md)

---

## Overview

All inter-node communication uses **gRPC with Protocol Buffers**. This document defines the service interfaces and message types.

### Why gRPC?

| Feature | Benefit |
|---------|---------|
| Strong typing | Protobuf schema = contract |
| Streaming | Real-time output streaming |
| Health checks | Standard gRPC health protocol |
| Code generation | Auto-generate Go clients/servers |
| Performance | Binary protocol, HTTP/2 |

---

## 1. Service Definitions

### 1.1 MasterService

Exposed by the master node for clients.

```protobuf
syntax = "proto3";

package distexec.v1;

import "google/protobuf/timestamp.proto";
import "google/protobuf/duration.proto";

// MasterService - API for clients to submit and monitor jobs
service MasterService {
    // Submit a new execution task
    rpc Submit(SubmitRequest) returns (SubmitResponse);
    
    // Get status of a submitted job
    rpc GetStatus(GetStatusRequest) returns (GetStatusResponse);
    
    // Cancel a running or pending job
    rpc Cancel(CancelRequest) returns (CancelResponse);
    
    // List jobs with optional filtering
    rpc ListJobs(ListJobsRequest) returns (ListJobsResponse);
    
    // Stream job output in real-time (optional for MVP)
    rpc StreamOutput(StreamOutputRequest) returns (stream OutputChunk);
}

message SubmitRequest {
    string task_type = 1;           // e.g., "command"
    bytes payload = 2;              // Task-specific payload (JSON)
    google.protobuf.Duration timeout = 3;
    int32 priority = 4;             // 0-10, higher = more urgent
    map<string, string> metadata = 5;
}

message SubmitResponse {
    string job_id = 1;
}

message GetStatusRequest {
    string job_id = 1;
}

message GetStatusResponse {
    string job_id = 1;
    JobStatus status = 2;
    bytes output = 3;               // Available output so far
    string error = 4;               // Error message if failed
    int32 exit_code = 5;            // For command tasks
    google.protobuf.Timestamp created_at = 6;
    google.protobuf.Timestamp started_at = 7;
    google.protobuf.Timestamp completed_at = 8;
    string worker_id = 9;           // Which worker is/was executing
}

enum JobStatus {
    JOB_STATUS_UNSPECIFIED = 0;
    JOB_STATUS_PENDING = 1;
    JOB_STATUS_RUNNING = 2;
    JOB_STATUS_COMPLETED = 3;
    JOB_STATUS_FAILED = 4;
    JOB_STATUS_CANCELLED = 5;
    JOB_STATUS_TIMEOUT = 6;
}

message CancelRequest {
    string job_id = 1;
}

message CancelResponse {
    bool cancelled = 1;
    string message = 2;
}

message ListJobsRequest {
    int32 limit = 1;                // Max jobs to return
    int32 offset = 2;               // Pagination offset
    JobStatus status_filter = 3;    // Filter by status (0 = all)
}

message ListJobsResponse {
    repeated JobSummary jobs = 1;
    int32 total = 2;                // Total matching jobs
}

message JobSummary {
    string job_id = 1;
    string task_type = 2;
    JobStatus status = 3;
    google.protobuf.Timestamp created_at = 4;
}

message StreamOutputRequest {
    string job_id = 1;
}

message OutputChunk {
    bytes data = 1;
    OutputType type = 2;
}

enum OutputType {
    OUTPUT_TYPE_STDOUT = 0;
    OUTPUT_TYPE_STDERR = 1;
}
```

### 1.2 WorkerService

Exposed by worker nodes for master to dispatch tasks.

```protobuf
// WorkerService - Called by master to dispatch tasks to workers
service WorkerService {
    // Execute a task on this worker
    rpc Execute(ExecuteRequest) returns (ExecuteResponse);
    
    // Execute with streaming output
    rpc ExecuteStream(ExecuteRequest) returns (stream ExecuteProgress);
    
    // Cancel a running task
    rpc CancelTask(CancelTaskRequest) returns (CancelTaskResponse);
    
    // Check worker health and capacity
    rpc GetWorkerStatus(GetWorkerStatusRequest) returns (GetWorkerStatusResponse);
}

message ExecuteRequest {
    string task_id = 1;
    string task_type = 2;           // "command", etc.
    bytes payload = 3;              // Task-specific (e.g., CommandPayload)
    google.protobuf.Duration timeout = 4;
    map<string, string> metadata = 5;
}

message ExecuteResponse {
    string task_id = 1;
    ExecutionStatus status = 2;
    bytes output = 3;
    string error = 4;
    int32 exit_code = 5;
    google.protobuf.Duration duration = 6;
}

enum ExecutionStatus {
    EXECUTION_STATUS_UNSPECIFIED = 0;
    EXECUTION_STATUS_SUCCESS = 1;
    EXECUTION_STATUS_FAILURE = 2;
    EXECUTION_STATUS_TIMEOUT = 3;
    EXECUTION_STATUS_CANCELLED = 4;
}

message ExecuteProgress {
    enum ProgressType {
        PROGRESS_STARTED = 0;
        PROGRESS_OUTPUT = 1;
        PROGRESS_COMPLETED = 2;
        PROGRESS_ERROR = 3;
    }
    ProgressType type = 1;
    bytes data = 2;                 // Output data for PROGRESS_OUTPUT
    ExecuteResponse final_result = 3; // Populated for PROGRESS_COMPLETED
}

message CancelTaskRequest {
    string task_id = 1;
}

message CancelTaskResponse {
    bool cancelled = 1;
}

message GetWorkerStatusRequest {}

message GetWorkerStatusResponse {
    string worker_id = 1;
    repeated string capabilities = 2;
    int32 max_concurrent_tasks = 3;
    int32 active_tasks = 4;
    WorkerHealth health = 5;
}

enum WorkerHealth {
    WORKER_HEALTH_UNKNOWN = 0;
    WORKER_HEALTH_HEALTHY = 1;
    WORKER_HEALTH_DEGRADED = 2;
    WORKER_HEALTH_UNHEALTHY = 3;
}
```

### 1.3 RegistrationService

For workers to register/heartbeat with master.

```protobuf
// RegistrationService - Workers call this to register and heartbeat
service RegistrationService {
    // Register a new worker node
    rpc Register(RegisterRequest) returns (RegisterResponse);
    
    // Deregister (graceful shutdown)
    rpc Deregister(DeregisterRequest) returns (DeregisterResponse);
    
    // Periodic heartbeat
    rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
}

message RegisterRequest {
    string worker_id = 1;           // Preferably pod name in K8s
    string address = 2;             // "host:port" where worker listens
    repeated string capabilities = 3;
    int32 max_concurrent_tasks = 4;
    map<string, string> labels = 5; // For affinity (e.g., "zone": "us-east-1a")
}

message RegisterResponse {
    bool accepted = 1;
    string message = 2;
    google.protobuf.Duration heartbeat_interval = 3; // How often to heartbeat
}

message DeregisterRequest {
    string worker_id = 1;
}

message DeregisterResponse {
    bool acknowledged = 1;
}

message HeartbeatRequest {
    string worker_id = 1;
    int32 active_tasks = 2;
    WorkerHealth health = 3;
    map<string, string> metrics = 4; // Optional: CPU, memory, etc.
}

message HeartbeatResponse {
    bool acknowledged = 1;
    // Master can send instructions back
    HeartbeatAction action = 2;
}

enum HeartbeatAction {
    HEARTBEAT_ACTION_NONE = 0;
    HEARTBEAT_ACTION_DRAIN = 1;     // Stop accepting new tasks
    HEARTBEAT_ACTION_SHUTDOWN = 2;  // Graceful shutdown
}
```

---

## 2. Task Payloads

Task payloads are JSON-encoded in the protobuf `bytes` field.

### 2.1 CommandPayload

```json
{
    "command": "ls",
    "args": ["-la", "/tmp"],
    "env": {
        "MY_VAR": "value"
    },
    "working_dir": "/home/user",
    "stdin": "optional input data"
}
```

Go struct:

```go
type CommandPayload struct {
    Command    string            `json:"command"`
    Args       []string          `json:"args,omitempty"`
    Env        map[string]string `json:"env,omitempty"`
    WorkingDir string            `json:"working_dir,omitempty"`
    Stdin      string            `json:"stdin,omitempty"`
}
```

### 2.2 HTTPPayload (Future)

```json
{
    "method": "POST",
    "url": "https://api.example.com/data",
    "headers": {
        "Content-Type": "application/json"
    },
    "body": "{\"key\": \"value\"}"
}
```

---

## 3. Error Handling

### gRPC Status Codes

| Scenario | Status Code | Description |
|----------|-------------|-------------|
| Invalid request | INVALID_ARGUMENT | Bad payload, missing fields |
| Job not found | NOT_FOUND | Unknown job ID |
| Worker unavailable | UNAVAILABLE | No healthy workers |
| Timeout | DEADLINE_EXCEEDED | Task timed out |
| Internal error | INTERNAL | Unexpected server error |
| Already cancelled | FAILED_PRECONDITION | Can't cancel completed job |

### Error Response Pattern

Always include details:

```go
return nil, status.Errorf(
    codes.NotFound,
    "job %s not found",
    req.JobId,
)
```

---

## 4. Streaming Considerations

For long-running commands, streaming is valuable:

```go
// Server-side streaming (master to client, worker to master)
func (s *server) StreamOutput(req *StreamOutputRequest, stream MasterService_StreamOutputServer) error {
    for {
        chunk, err := s.getNextChunk(req.JobId)
        if err == io.EOF {
            return nil
        }
        if err != nil {
            return err
        }
        if err := stream.Send(chunk); err != nil {
            return err
        }
    }
}
```

**Research:** Look at how kubectl logs uses streaming, or how HashiCorp Nomad streams job output.

---

## 5. Health Checking

Use the standard gRPC health checking protocol:

```protobuf
// From grpc-health-probe
package grpc.health.v1;

service Health {
    rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
    rpc Watch(HealthCheckRequest) returns (stream HealthCheckResponse);
}
```

**Implementation:** Use `google.golang.org/grpc/health` package.

This enables K8s readiness/liveness probes:

```yaml
readinessProbe:
  exec:
    command: ["grpc-health-probe", "-addr=:50051"]
  initialDelaySeconds: 5
livenessProbe:
  exec:
    command: ["grpc-health-probe", "-addr=:50051"]
  initialDelaySeconds: 10
```

---

## 6. File Organization

```
proto/
├── distexec/
│   └── v1/
│       ├── master.proto      # MasterService
│       ├── worker.proto      # WorkerService
│       ├── registration.proto # RegistrationService
│       └── common.proto      # Shared types (JobStatus, etc.)
└── buf.yaml                  # Buf build config
```

**Using Buf (recommended):**

```yaml
# buf.yaml
version: v1
name: buf.build/yourname/distexec
deps:
  - buf.build/protocolbuffers/wellknowntypes
```

```yaml
# buf.gen.yaml
version: v1
plugins:
  - plugin: go
    out: gen/go
    opt: paths=source_relative
  - plugin: go-grpc
    out: gen/go
    opt: paths=source_relative
```

Generate with: `buf generate`

**Research:** Look at how etcd, CockroachDB, or Vitess organize their protobufs.
