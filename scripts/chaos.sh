#!/bin/bash
# SentinelFS Chaos Script — Trigger degradation on a node
# Usage: ./scripts/chaos.sh [node-port] [speed]
#   node-port: admin port (default: 9503 = node-3)
#   speed: degradation speed (default: 2.0)

PORT=${1:-9503}
SPEED=${2:-2.0}
ACTION=${3:-degrade}

case $ACTION in
    degrade)
        echo "🔥 Triggering degradation on port $PORT (speed=$SPEED)"
        curl -s -X POST "http://localhost:$PORT/degrade?speed=$SPEED"
        echo ""
        ;;
    recover)
        echo "✅ Stopping degradation on port $PORT"
        curl -s -X POST "http://localhost:$PORT/recover"
        echo ""
        ;;
    status)
        echo "📊 Node status on port $PORT"
        curl -s "http://localhost:$PORT/status"
        echo ""
        ;;
    *)
        echo "Usage: $0 [port] [speed] [degrade|recover|status]"
        ;;
esac