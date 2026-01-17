# Dynamic Scaling & Kubernetes Integration

**Parent:** [00-OVERVIEW.md](./00-OVERVIEW.md)

---

## Overview

The system supports **dynamic cluster scaling** with the Master node directly controlling worker capacity. When all workers are busy or queue is backing up, the Master proactively spins up new workers via the K8s API. This provides **immediate, demand-driven scaling** rather than relying solely on reactive HPA metrics.

---

## 1. Core Principle: Stateless Workers

Workers are **stateless and replaceable**. This is the key enabler for dynamic scaling.

```
Worker Pod = gRPC Server + Executor Plugins

- No persistent state
- No local job queue
- All coordination via master
- Startup: register with master
- Shutdown: deregister, drain tasks
```

### What This Means

| Component | Stateless? | Notes |
|-----------|------------|-------|
| Worker pods | ✅ Yes | Can be killed/replaced anytime |
| Master node | ❌ No | Holds job queue, node registry |
| Task data | In-memory | Simple for MVP, can add Redis later |

---

## 2. Kubernetes Deployment Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                            KUBERNETES CLUSTER                            │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌────────────────────────────────┐    ┌─────────────────────────────┐  │
│  │         Deployment:            │    │       Deployment:           │  │
│  │       master (replicas: 1)     │    │   worker (replicas: 3-10)   │  │
│  │  ┌──────────────────────────┐  │    │  ┌───────────────────────┐  │  │
│  │  │    Master Pod            │  │    │  │   Worker Pod          │  │  │
│  │  │    ┌──────────────────┐  │  │    │  │   ┌────────────────┐  │  │  │
│  │  │    │ master container │  │  │    │  │   │ worker        │  │  │  │
│  │  │    │ :50051 (gRPC)    │  │  │    │  │   │ container     │  │  │  │
│  │  │    └──────────────────┘  │  │    │  │   │ :50052        │  │  │  │
│  │  └──────────────────────────┘  │    │  │   └────────────────┘  │  │  │
│  └────────────────────────────────┘    │  └───────────────────────┘  │  │
│                  │                      │            │               │  │
│                  ▼                      └────────────┼───────────────┘  │
│  ┌───────────────────────────────┐                   │                  │
│  │      Service: master          │                   │                  │
│  │   (ClusterIP, port 50051)     │                   │                  │
│  └───────────────────────────────┘                   │                  │
│                                                      ▼                  │
│                               ┌───────────────────────────────────────┐ │
│                               │     Headless Service: workers         │ │
│                               │  (Returns all pod IPs, port 50052)    │ │
│                               └───────────────────────────────────────┘ │
│                                                                          │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                   HorizontalPodAutoscaler                        │    │
│  │                   (target: worker deployment)                    │    │
│  │                   (metric: custom queue depth)                   │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Service Discovery

### Option A: Headless Service (Recommended for MVP)

Workers use a headless service - each worker pod gets its own DNS entry.

```yaml
# worker-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: dist-exec-workers
spec:
  clusterIP: None  # Headless!
  selector:
    app: dist-exec-worker
  ports:
    - port: 50052
      targetPort: grpc
```

DNS query for `dist-exec-workers.default.svc.cluster.local` returns all pod IPs.

**Worker Registration Flow:**

1. Worker pod starts
2. Determines own IP via downward API or hostname resolution
3. Calls `Register(workerID, myIP:50052, capabilities)` to master
4. Master adds to node registry
5. Worker begins heartbeat loop

**Pros:**
- Simple for MVP
- Workers self-register

**Cons:**
- Master must track workers
- Stale registrations if deregister fails

### Option B: Kubernetes API Watch (Advanced)

Master watches K8s API for pod events:

```go
podWatcher, err := clientset.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
    LabelSelector: "app=dist-exec-worker",
})
for event := range podWatcher.ResultChan() {
    switch event.Type {
    case watch.Added:
        // New worker pod, register it
    case watch.Deleted:
        // Worker gone, remove from registry
    }
}
```

**Pros:**
- More reliable (K8s is source of truth)
- No need for worker to call Register

**Cons:**
- Requires K8s API access (RBAC)
- More complex

**Recommendation:** Start with Option A, upgrade to B later.

---

## 4. Dynamic Node Addition

### How Workers Join

```
┌──────────────┐     ┌───────────────────┐     ┌──────────────┐
│ New Worker   │────▶│  Register(self)   │────▶│    Master    │
│    Pod       │     │   to Master       │     │  NodeRegistry│
└──────────────┘     └───────────────────┘     └──────┬───────┘
                                                       │
                                                       ▼
                                               Worker added to
                                               load balancer pool
                                                       │
                                                       ▼
                                               Immediately starts
                                               receiving tasks
```

### Worker Startup Sequence

```go
func (w *Worker) Start(ctx context.Context) error {
    // 1. Start gRPC server
    go w.grpcServer.Serve(listener)
    
    // 2. Wait for server to be ready
    <-w.serverReady
    
    // 3. Register with master
    resp, err := w.masterClient.Register(ctx, &RegisterRequest{
        WorkerId:     w.id,
        Address:      w.address,
        Capabilities: w.capabilities,
        MaxConcurrentTasks: w.maxTasks,
    })
    if err != nil {
        return fmt.Errorf("failed to register: %w", err)
    }
    
    // 4. Start heartbeat loop
    go w.heartbeatLoop(ctx, resp.HeartbeatInterval.AsDuration())
    
    log.Info("worker registered and ready", "id", w.id)
    return nil
}
```

### Master-Driven Scaling (Primary Approach)

The Master node directly calls the Kubernetes API to scale workers. This provides **immediate, proactive scaling** when capacity is needed.

#### Why Master-Driven?

| Approach | Latency | Control |
|----------|---------|---------|
| HPA (reactive) | 30-60s | K8s decides |
| **Master-driven** | **5-20s** | **Master decides** |

#### Implementation

```go
package master

import (
    "context"
    "fmt"
    "sync"
    "time"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

type AutoScaler struct {
    k8s         *kubernetes.Clientset
    namespace   string
    deployment  string  // "dist-exec-worker"
    registry    *NodeRegistry
    cfg         ScalerConfig
    
    mu          sync.Mutex
    lastScaleUp time.Time
}

type ScalerConfig struct {
    MinReplicas      int           // e.g., 3
    MaxReplicas      int           // e.g., 20
    TasksPerWorker   int           // e.g., 10
    ScaleUpThreshold int           // pending tasks per worker to trigger scale
    CooldownPeriod   time.Duration // e.g., 30s between scale operations
}

// EnsureCapacity is called before dispatching tasks
func (s *AutoScaler) EnsureCapacity(ctx context.Context, pendingTasks int) error {
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
        totalCapacity += n.MaxTasks - n.ActiveTasks
    }
    
    // CASE 1: No capacity and tasks pending → Scale up immediately
    if totalCapacity == 0 && pendingTasks > 0 {
        needed := min(
            (pendingTasks/s.cfg.TasksPerWorker)+1,
            s.cfg.MaxReplicas-currentReplicas,
        )
        if needed > 0 {
            return s.scaleUp(ctx, currentReplicas+needed)
        }
    }
    
    // CASE 2: Queue backing up → Proactive scale up
    if pendingTasks > currentReplicas*s.cfg.ScaleUpThreshold {
        if currentReplicas < s.cfg.MaxReplicas {
            return s.scaleUp(ctx, currentReplicas+1)
        }
    }
    
    return nil
}

func (s *AutoScaler) scaleUp(ctx context.Context, desired int) error {
    deploy, err := s.k8s.AppsV1().Deployments(s.namespace).Get(
        ctx, s.deployment, metav1.GetOptions{},
    )
    if err != nil {
        return fmt.Errorf("get deployment: %w", err)
    }
    
    replicas := int32(desired)
    deploy.Spec.Replicas = &replicas
    
    _, err = s.k8s.AppsV1().Deployments(s.namespace).Update(
        ctx, deploy, metav1.UpdateOptions{},
    )
    if err != nil {
        return fmt.Errorf("update deployment: %w", err)
    }
    
    s.lastScaleUp = time.Now()
    log.Info("scaled up workers", "replicas", desired)
    return nil
}

// ScaleDown removes idle workers (called periodically)
func (s *AutoScaler) ScaleDown(ctx context.Context) error {
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
        return s.setReplicas(ctx, currentReplicas-1)
    }
    
    return nil
}
```

#### Integration in Dispatcher

The scaler is called before dispatching:

```go
func (d *Dispatcher) dispatchLoop(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case task := <-d.scheduler.Ready():
            // Ensure we have capacity BEFORE waiting for a node
            pendingCount := d.scheduler.PendingCount()
            if err := d.scaler.EnsureCapacity(ctx, pendingCount); err != nil {
                log.Warn("scale up failed", "error", err)
            }
            
            // Select node and dispatch
            node, err := d.balancer.Select(task, d.registry.GetHealthyNodes())
            if err != nil {
                // Still no nodes? Retry after delay
                d.scheduler.Requeue(task)
                continue
            }
            
            go d.dispatch(ctx, node, task)
        }
    }
}
```

#### RBAC for K8s API Access

The master needs permissions to scale the worker deployment:

```yaml
# master-rbac.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: dist-exec-master
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: dist-exec-scaler
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "update", "patch"]
  - apiGroups: ["apps"]
    resources: ["deployments/scale"]
    verbs: ["get", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: dist-exec-master-scaler
subjects:
  - kind: ServiceAccount
    name: dist-exec-master
roleRef:
  kind: Role
  name: dist-exec-scaler
  apiGroup: rbac.authorization.k8s.io
```

---

### Kubernetes HPA (Secondary/Backup)

HPA can be used as a safety net alongside master-driven scaling:

```yaml
# worker-hpa.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: dist-exec-worker-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: dist-exec-worker
  minReplicas: 3
  maxReplicas: 20
  metrics:
    # CPU-based as backup
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 80
```

> [!NOTE]
> When using both HPA and master-driven scaling, be aware they can conflict. The master should check current HPA desired replicas and not scale below it.

---

## 5. Graceful Shutdown

When K8s terminates a worker pod:

```
┌─────────────────────────────────────────────────────────────────┐
│                     GRACEFUL SHUTDOWN                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. K8s sends SIGTERM                                            │
│           │                                                      │
│           ▼                                                      │
│  2. Worker stops heartbeat                                       │
│           │                                                      │
│           ▼                                                      │
│  3. Worker calls Deregister()                                    │
│           │                                                      │
│           ▼                                                      │
│  4. Master removes from load balancer (no new tasks)             │
│           │                                                      │
│           ▼                                                      │
│  5. Worker waits for in-flight tasks (draining)                  │
│           │                                                      │
│           ▼                                                      │
│  6. gRPC server graceful stop                                    │
│           │                                                      │
│           ▼                                                      │
│  7. Pod terminates                                               │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Implementation

```go
func (w *Worker) Shutdown(ctx context.Context) error {
    log.Info("shutdown initiated", "id", w.id)
    
    // 1. Stop accepting new tasks via health check
    w.health.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
    
    // 2. Deregister from master
    _, err := w.masterClient.Deregister(ctx, &DeregisterRequest{WorkerId: w.id})
    if err != nil {
        log.Error("deregister failed", "error", err)
    }
    
    // 3. Wait for in-flight tasks
    log.Info("draining tasks", "active", w.taskRunner.ActiveCount())
    w.taskRunner.WaitForDrain(ctx)
    
    // 4. Stop gRPC server
    w.grpcServer.GracefulStop()
    
    return nil
}
```

### K8s Configuration

```yaml
# worker-deployment.yaml (relevant parts)
spec:
  template:
    spec:
      terminationGracePeriodSeconds: 60  # Time for draining
      containers:
        - name: worker
          lifecycle:
            preStop:
              exec:
                command: ["/bin/sh", "-c", "sleep 5"]  # Let LB drain
```

---

## 6. Failure Handling

### Worker Crash Detection

Master detects crashed workers via missed heartbeats:

```go
func (r *NodeRegistry) monitorHealth(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Second)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            r.mu.Lock()
            for id, node := range r.nodes {
                if time.Since(node.LastSeen) > 15*time.Second {
                    log.Warn("node unhealthy", "id", id)
                    node.Status = NodeStatus_UNHEALTHY
                    r.rescheduleNodeTasks(id) // Reassign tasks
                }
                if time.Since(node.LastSeen) > 30*time.Second {
                    log.Error("node dead, removing", "id", id)
                    delete(r.nodes, id)
                    r.onNodeRemoved(id)
                }
            }
            r.mu.Unlock()
        }
    }
}
```

### Task Reassignment

When a worker dies mid-task:

1. Master detects missed heartbeats
2. Marks all tasks on that worker as FAILED
3. Scheduler re-enqueues tasks with `retries < maxRetries`
4. Tasks dispatched to healthy workers

---

## 7. Deployment Manifests

### Master Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dist-exec-master
spec:
  replicas: 1  # Single master for MVP
  selector:
    matchLabels:
      app: dist-exec-master
  template:
    metadata:
      labels:
        app: dist-exec-master
    spec:
      containers:
        - name: master
          image: dist-exec-master:latest
          ports:
            - name: grpc
              containerPort: 50051
          env:
            - name: LOG_LEVEL
              value: "info"
          readinessProbe:
            exec:
              command: ["grpc-health-probe", "-addr=:50051"]
            initialDelaySeconds: 5
          livenessProbe:
            exec:
              command: ["grpc-health-probe", "-addr=:50051"]
            initialDelaySeconds: 10
          resources:
            requests:
              cpu: "250m"
              memory: "256Mi"
            limits:
              cpu: "1"
              memory: "512Mi"
---
apiVersion: v1
kind: Service
metadata:
  name: dist-exec-master
spec:
  selector:
    app: dist-exec-master
  ports:
    - port: 50051
      targetPort: grpc
```

### Worker Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dist-exec-worker
spec:
  replicas: 3  # Starting replicas
  selector:
    matchLabels:
      app: dist-exec-worker
  template:
    metadata:
      labels:
        app: dist-exec-worker
    spec:
      terminationGracePeriodSeconds: 60
      containers:
        - name: worker
          image: dist-exec-worker:latest
          ports:
            - name: grpc
              containerPort: 50052
          env:
            - name: MASTER_ADDRESS
              value: "dist-exec-master:50051"
            - name: WORKER_ID
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: MAX_CONCURRENT_TASKS
              value: "10"
          readinessProbe:
            exec:
              command: ["grpc-health-probe", "-addr=:50052"]
            initialDelaySeconds: 5
          resources:
            requests:
              cpu: "500m"
              memory: "256Mi"
            limits:
              cpu: "2"
              memory: "1Gi"
---
apiVersion: v1
kind: Service
metadata:
  name: dist-exec-workers
spec:
  clusterIP: None  # Headless
  selector:
    app: dist-exec-worker
  ports:
    - port: 50052
      targetPort: grpc
```

---

## 8. Local Development Without K8s

For local testing, use Docker Compose or direct processes:

```yaml
# docker-compose.yml
version: '3.8'
services:
  master:
    build: ./cmd/master
    ports:
      - "50051:50051"
    environment:
      - LOG_LEVEL=debug

  worker-1:
    build: ./cmd/worker
    environment:
      - MASTER_ADDRESS=master:50051
      - WORKER_ID=worker-1
    depends_on:
      - master

  worker-2:
    build: ./cmd/worker
    environment:
      - MASTER_ADDRESS=master:50051
      - WORKER_ID=worker-2
    depends_on:
      - master
```

Scale workers: `docker-compose up --scale worker=5`

---

## Research Links for Implementers

- **Kubernetes Go Client:** https://github.com/kubernetes/client-go
- **grpc-health-probe:** https://github.com/grpc-ecosystem/grpc-health-probe
- **Custom Metrics HPA:** https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/#scaling-on-custom-metrics
- **Prometheus Adapter:** https://github.com/kubernetes-sigs/prometheus-adapter
- **Graceful Shutdown in K8s:** https://blog.sebastian-daschner.com/entries/zero-downtime-updates-kubernetes
