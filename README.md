# Distributed Command Executor

A Kubernetes-native system for executing commands at scale across distributed worker nodes.

## Features

-  **Parallel Execution** - Run commands across multiple workers simultaneously
-  **Kubernetes Native** - First-class CRD support with `kubectl` integration
-  **Auto-Scaling** - Dynamically scales workers based on queue depth
-  **Fast** - gRPC-based communication for minimal latency
-  **Secure** - Command allowlists/blocklists, RBAC integration

## Quick Start

```bash
# Submit a job via CLI
./bin/distctl run echo "Hello, World!"

# Or via Kubernetes CRD
kubectl apply -f - <<EOF
apiVersion: distexec.io/v1
kind: DistributedJob
metadata:
  name: hello
spec:
  command: "echo"
  args: ["Hello from K8s!"]
  parallelism: 5
EOF

# Check status
kubectl get distributedjobs
./bin/distctl list
```

## Installation

```bash
# Build
make build

# Deploy to Kubernetes
kubectl apply -f deploy/k8s/
```

## Documentation

| Topic | Link |
|-------|------|
| Architecture | [specs/00-OVERVIEW.md](specs/00-OVERVIEW.md) |
| Components | [specs/01-COMPONENTS.md](specs/01-COMPONENTS.md) |
| API Reference | [specs/02-RPC-PROTOCOL.md](specs/02-RPC-PROTOCOL.md) |
| Scaling | [specs/03-DYNAMIC-SCALING.md](specs/03-DYNAMIC-SCALING.md) |

## License

MIT
