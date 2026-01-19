#!/bin/bash
# local-cluster.sh - Manage local development cluster
set -e

cd "$(dirname "$0")/.."

usage() {
    echo "Usage: $0 {start|stop|restart|logs|status}"
    echo ""
    echo "Commands:"
    echo "  start   - Start master and worker nodes"
    echo "  stop    - Stop all nodes"
    echo "  restart - Restart all nodes"
    echo "  logs    - Show logs from all nodes"
    echo "  status  - Check running processes"
    exit 1
}

start_master() {
    echo "Starting master..."
    MASTER_ADDRESS=:50051 LOG_LEVEL=debug go run ./cmd/master &
    echo $! > /tmp/distexec-master.pid
    echo "Master started (PID: $(cat /tmp/distexec-master.pid))"
}

start_worker() {
    local id=$1
    local port=$2
    echo "Starting worker-$id on port $port..."
    WORKER_ID=worker-$id WORKER_ADDRESS=:$port MASTER_ADDRESS=localhost:50051 LOG_LEVEL=debug go run ./cmd/worker &
    echo $! > /tmp/distexec-worker-$id.pid
    echo "Worker-$id started (PID: $(cat /tmp/distexec-worker-$id.pid))"
}

stop_all() {
    echo "Stopping all nodes..."
    for pidfile in /tmp/distexec-*.pid; do
        if [ -f "$pidfile" ]; then
            pid=$(cat "$pidfile")
            if kill -0 "$pid" 2>/dev/null; then
                kill "$pid"
                echo "Stopped process $pid"
            fi
            rm "$pidfile"
        fi
    done
    echo "All nodes stopped"
}

show_status() {
    echo "Running nodes:"
    for pidfile in /tmp/distexec-*.pid; do
        if [ -f "$pidfile" ]; then
            pid=$(cat "$pidfile")
            name=$(basename "$pidfile" .pid | sed 's/distexec-//')
            if kill -0 "$pid" 2>/dev/null; then
                echo "  $name: running (PID: $pid)"
            else
                echo "  $name: stopped (stale PID file)"
            fi
        fi
    done
}

case "${1:-}" in
    start)
        # Build first
        echo "Building..."
        go build ./...
        
        start_master
        sleep 1  # Wait for master to be ready
        start_worker 1 50052
        start_worker 2 50053
        
        echo ""
        echo "Cluster started! Use 'scripts/local-cluster.sh logs' to view logs."
        ;;
    stop)
        stop_all
        ;;
    restart)
        stop_all
        sleep 1
        $0 start
        ;;
    logs)
        echo "Use docker-compose logs for containerized setup."
        echo "For manual setup, check terminal output."
        ;;
    status)
        show_status
        ;;
    *)
        usage
        ;;
esac
