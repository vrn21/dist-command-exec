# Distributed Command Executor

A scalable distributed system for executing commands across multiple worker nodes, built with Go and gRPC.

## Features

- **Master-Worker Architecture** - Central coordination with horizontal worker scaling
- **gRPC Communication** - Fast, type-safe inter-node communication
- **Dynamic Scaling** - Kubernetes-native auto-scaling based on queue depth
- **Pluggable Executors** - Extensible task execution (command, HTTP, etc.)
- **Graceful Shutdown** - Task draining and proper cleanup

## Quick Start

### Prerequisites

- Go 1.21+
- [buf](https://buf.build/) for protobuf generation
- Docker & Docker Compose (for local development)

### Build

```bash
# Generate protobuf code and build binaries
make all

# Or step by step:
make proto   # Generate protobuf code
make build   # Build binaries
```

### Test

```bash
make test            # Run tests with race detection
make test-coverage   # Generate coverage report
```

### Run Locally

```bash
# Using Docker Compose (recommended)
make local-up

# Or manually in separate terminals:
make run-master   # Terminal 1
make run-worker   # Terminal 2
```

### Execute Commands

```bash
# Using CLI
./bin/distctl run --command "echo" --args "hello world"

# Check status
./bin/distctl status <job-id>
```

## Configuration

### Master

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `MASTER_ADDRESS` | `:50051` | gRPC listen address |
| `MAX_CONCURRENT_JOBS` | `1000` | Maximum queued jobs |
| `HEARTBEAT_TIMEOUT_SECONDS` | `15` | Worker heartbeat timeout |
| `LOG_LEVEL` | `info` | Logging level |

### Worker

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `WORKER_ID` | hostname | Unique worker ID |
| `WORKER_ADDRESS` | `:50052` | gRPC listen address |
| `MASTER_ADDRESS` | (required) | Master node address |
| `MAX_CONCURRENT_TASKS` | `10` | Max parallel tasks |
| `EXECUTOR_COMMAND_ENABLED` | `true` | Enable command executor |
| `EXECUTOR_COMMAND_ALLOWED` | (empty) | Comma-separated allowed commands |
| `EXECUTOR_COMMAND_BLOCKED` | (empty) | Comma-separated blocked commands |

## Architecture

```
┌─────────────┐         ┌─────────────┐
│   Client    │────────▶│   Master    │
│  (distctl)  │  gRPC   │  (scheduler,│
└─────────────┘         │  registry)  │
                        └──────┬──────┘
                               │ gRPC
              ┌────────────────┼────────────────┐
              ▼                ▼                ▼
        ┌──────────┐    ┌──────────┐    ┌──────────┐
        │ Worker 1 │    │ Worker 2 │    │ Worker N │
        │(executor)│    │(executor)│    │(executor)│
        └──────────┘    └──────────┘    └──────────┘
```

See [specs/](./specs/) for detailed design documents:
- [00-OVERVIEW.md](./specs/00-OVERVIEW.md) - Architecture overview
- [01-COMPONENTS.md](./specs/01-COMPONENTS.md) - Component details
- [02-RPC-PROTOCOL.md](./specs/02-RPC-PROTOCOL.md) - Protocol definitions
- [03-DYNAMIC-SCALING.md](./specs/03-DYNAMIC-SCALING.md) - Auto-scaling design
- [04-EXECUTOR-PLUGIN.md](./specs/04-EXECUTOR-PLUGIN.md) - Executor interface
- [05-PROJECT-STRUCTURE.md](./specs/05-PROJECT-STRUCTURE.md) - Code organization

## Kubernetes Deployment

```bash
kubectl apply -f deploy/k8s/
```

See [deploy/k8s/](./deploy/k8s/) for manifests.

## License

MIT
