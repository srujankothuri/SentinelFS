#!/bin/bash
# SentinelFS Demo — Shows predictive self-healing in action

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

echo "═══════════════════════════════════════════════════"
echo "        SentinelFS 🛡️  Live Demo                   "
echo "═══════════════════════════════════════════════════"
echo ""

META=${SENTINEL_META_ADDR:-localhost:9000}
export SENTINEL_META_ADDR=$META

echo -e "${CYAN}Step 1: Check cluster health${NC}"
go run ./cmd/client cluster
echo ""

echo -e "${CYAN}Step 2: Upload test files${NC}"
for i in 1 2 3; do
    dd if=/dev/urandom of=/tmp/demo$i.bin bs=1024 count=512 2>/dev/null
    go run ./cmd/client put /tmp/demo$i.bin /data/demo$i.bin
done
echo ""

echo -e "${CYAN}Step 3: Show file distribution${NC}"
go run ./cmd/client ls /
echo ""
go run ./cmd/client info /data/demo1.bin
echo ""

echo -e "${CYAN}Step 4: Show node health${NC}"
go run ./cmd/client nodes
echo ""

echo -e "${YELLOW}Step 5: 🔥 Triggering disk degradation on node-3...${NC}"
curl -s -X POST "http://localhost:9503/degrade?speed=2.0" | python3 -m json.tool 2>/dev/null || curl -s -X POST "http://localhost:9503/degrade?speed=2.0"
echo ""

echo -e "${CYAN}Step 6: Watch cluster heal itself (Ctrl+C to stop)${NC}"
echo ""
go run ./cmd/client watch 2s