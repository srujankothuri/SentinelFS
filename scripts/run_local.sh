#!/bin/bash
# Start a local SentinelFS cluster with 5 data nodes
# Usage: ./scripts/run_local.sh [start|stop]

ACTION=${1:-start}

case $ACTION in
    start)
        echo "🛡️  Starting SentinelFS cluster..."

        # Kill any existing processes
        pkill -9 -f "metaserver\|datanode" 2>/dev/null || true
        kill -9 $(lsof -ti :9000-9005,9500-9505,9600) 2>/dev/null || true
        sleep 2
        rm -rf data/

        # Start metaserver
        go run ./cmd/metaserver --port 9000 --admin-port 9600 &
        sleep 2

        # Start 5 data nodes
        for i in 1 2 3 4 5; do
            PORT=$((9000 + i))
            ADMIN=$((9500 + i))
            go run ./cmd/datanode --meta-addr localhost:9000 --port $PORT --admin-port $ADMIN --data-dir ./data/node$i &
        done
        sleep 3

        echo ""
        echo "✅ Cluster running!"
        echo ""
        echo "Usage:"
        echo "  go run ./cmd/client put <local> <remote>    Upload a file"
        echo "  go run ./cmd/client get <remote> <local>    Download a file"
        echo "  go run ./cmd/client cluster                 Cluster status"
        echo "  go run ./cmd/client nodes                   Node health"
        echo "  go run ./cmd/client watch                   Live dashboard"
        echo ""
        echo "Chaos testing:"
        echo "  curl -X POST 'http://localhost:9503/degrade?speed=2.0'"
        echo "  curl -X POST 'http://localhost:9503/recover'"
        echo "  curl http://localhost:9600/migrations"
        echo ""
        echo "Stop:  ./scripts/run_local.sh stop"
        ;;

    stop)
        echo "Stopping SentinelFS cluster..."
        pkill -9 -f "metaserver\|datanode" 2>/dev/null || true
        kill -9 $(lsof -ti :9000-9005,9500-9505,9600) 2>/dev/null || true
        rm -rf data/
        echo "✅ Stopped."
        ;;

    *)
        echo "Usage: $0 [start|stop]"
        ;;
esac