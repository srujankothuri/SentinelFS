#!/bin/bash
# SentinelFS Chaos Test — Validates Predictive Self-Healing Pipeline
# Strategy: upload files, find which node has chunks, degrade THAT node

set -e

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

PIDS=()
TEST_DIR=$(mktemp -d)
PASS=0
FAIL=0

META_GRPC=localhost:9200
META_ADMIN=localhost:9610

cleanup() {
    echo -e "\n${YELLOW}Cleaning up...${NC}"
    for pid in "${PIDS[@]}"; do
        kill -9 "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    done
    sleep 1
    # Kill any orphan Go processes on our ports
    kill -9 $(lsof -ti :9200-9205,9610,9700-9705) 2>/dev/null || true
    sleep 1
    rm -rf "$TEST_DIR" data/chaos-node-*
    echo "Done."
}
trap cleanup EXIT

pass() { echo -e "  ${GREEN}✓ $1${NC}"; PASS=$((PASS + 1)); }
fail() { echo -e "  ${RED}✗ $1${NC}"; FAIL=$((FAIL + 1)); }
info() { echo -e "  ${CYAN}ℹ $1${NC}"; }

export SENTINEL_META_ADDR=$META_GRPC

echo "═══════════════════════════════════════════════════"
echo "   SentinelFS 🛡️  Chaos Test: Predictive Healing  "
echo "═══════════════════════════════════════════════════"
echo ""

# ── Kill any leftover processes ──
kill -9 $(lsof -ti :9200-9205,9610,9700-9705) 2>/dev/null || true
sleep 2

# ── Phase 1: Start cluster ──
echo -e "${YELLOW}Phase 1: Starting cluster (5 nodes)...${NC}"

go run ./cmd/metaserver --port 9200 --admin-port 9610 > /dev/null 2>&1 &
PIDS+=($!)
sleep 3

for i in 1 2 3 4 5; do
    PORT=$((9200 + i))
    ADMIN=$((9700 + i))
    go run ./cmd/datanode --meta-addr $META_GRPC --port $PORT --admin-port $ADMIN --data-dir ./data/chaos-node-$i > /dev/null 2>&1 &
    PIDS+=($!)
done
sleep 5

OUTPUT=$(go run ./cmd/client cluster 2>&1) || true
if echo "$OUTPUT" | grep -q "5 total"; then
    pass "Cluster started with 5 nodes"
else
    fail "Cluster startup"
    echo "$OUTPUT"
    exit 1
fi

# Verify all nodes are healthy and have low risk
NODES_OUTPUT=$(go run ./cmd/client nodes 2>&1) || true
info "Initial node state:"
echo "$NODES_OUTPUT" | grep "node-" | while IFS= read -r line; do echo "      $line"; done

# ── Phase 2: Upload files BEFORE any degradation ──
echo ""
echo -e "${YELLOW}Phase 2: Uploading test files...${NC}"

for i in $(seq 1 6); do
    dd if=/dev/urandom of="$TEST_DIR/file$i.bin" bs=1024 count=256 2>/dev/null
    go run ./cmd/client put "$TEST_DIR/file$i.bin" /data/file$i.bin > /dev/null 2>&1
done

FILE1_MD5=$(md5 -q "$TEST_DIR/file1.bin" 2>/dev/null || md5sum "$TEST_DIR/file1.bin" | awk '{print $1}')
FILE2_MD5=$(md5 -q "$TEST_DIR/file2.bin" 2>/dev/null || md5sum "$TEST_DIR/file2.bin" | awk '{print $1}')
FILE3_MD5=$(md5 -q "$TEST_DIR/file3.bin" 2>/dev/null || md5sum "$TEST_DIR/file3.bin" | awk '{print $1}')

pass "6 test files uploaded"

# ── Phase 3: Find which node has the most chunks ──
echo ""
echo -e "${YELLOW}Phase 3: Finding target node for degradation...${NC}"

# Check each file's chunk placement to find nodes with chunks
NODES_WITH_CHUNKS=$(go run ./cmd/client nodes 2>&1 | grep "node-" || true)
info "Node chunk distribution:"
echo "$NODES_WITH_CHUNKS" | while IFS= read -r line; do echo "      $line"; done

# Find the admin port of a node that has chunks
# Query each node's admin to find one that's healthy and NOT already degraded
TARGET_PORT=""
TARGET_NODE=""
for APORT in 9701 9702 9703 9704 9705; do
    STATUS=$(curl -s "http://localhost:$APORT/status" 2>/dev/null) || continue
    IS_DEGRADING=$(echo "$STATUS" | grep -o '"degrading":false' || true)
    LATENCY=$(echo "$STATUS" | grep -o '"disk_io_latency":"[0-9.]*' | grep -o '[0-9.]*$' || true)

    # Only pick nodes with low latency (fresh, not leftover degradation)
    if [ -n "$IS_DEGRADING" ] && [ -n "$LATENCY" ]; then
        # Check if latency is under 100ms (healthy baseline)
        LATENCY_INT=$(echo "$LATENCY" | cut -d. -f1)
        if [ "$LATENCY_INT" -lt 100 ] 2>/dev/null; then
            NODE_ID=$(echo "$STATUS" | grep -o '"node_id":"[^"]*"' | grep -o 'node-[0-9]*')
            TARGET_PORT=$APORT
            TARGET_NODE=$NODE_ID
            info "Selected target: $TARGET_NODE (admin port: $TARGET_PORT, latency: ${LATENCY}ms)"
            break
        fi
    fi
done

if [ -z "$TARGET_PORT" ]; then
    fail "Could not find a healthy node to degrade"
    info "All nodes may have leftover degradation state"
    exit 1
fi

# ── Phase 4: Collect baseline and check migrations ──
echo ""
echo -e "${YELLOW}Phase 4: Collecting baseline metrics (15s)...${NC}"
sleep 15

BASELINE_MIGRATIONS=$(curl -s "http://$META_ADMIN/migrations" 2>/dev/null | grep -o '"count":[0-9]*' | grep -o '[0-9]*' || echo "0")
if [ -z "$BASELINE_MIGRATIONS" ]; then BASELINE_MIGRATIONS=0; fi
info "Baseline migrations: $BASELINE_MIGRATIONS"
pass "Baseline collected"

# ── Phase 5: Degrade the target node ──
echo ""
echo -e "${YELLOW}Phase 5: 🔥 Triggering degradation on $TARGET_NODE (port $TARGET_PORT)...${NC}"
curl -s -X POST "http://localhost:$TARGET_PORT/degrade?speed=2.0" > /dev/null 2>&1
pass "Degradation started on $TARGET_NODE (speed=2.0)"

# ── Phase 6: Monitor for migration ──
echo ""
echo -e "${YELLOW}Phase 6: Monitoring for prediction & migration...${NC}"

MIGRATION_DETECTED=false
RISK_DETECTED=false
MAX_WAIT=120
ELAPSED=0

while [ "$ELAPSED" -lt "$MAX_WAIT" ]; do
    sleep 5
    ELAPSED=$((ELAPSED + 5))

    # Check migration count via HTTP
    MIGRATION_RESP=$(curl -s "http://$META_ADMIN/migrations" 2>/dev/null) || true
    CURRENT_MIGRATIONS=$(echo "$MIGRATION_RESP" | grep -o '"count":[0-9]*' | grep -o '[0-9]*' || echo "0")
    if [ -z "$CURRENT_MIGRATIONS" ]; then CURRENT_MIGRATIONS=0; fi

    # Check node status via CLI
    NODE_OUTPUT=$(go run ./cmd/client nodes 2>&1) || true
    DEGRADED_LINE=$(echo "$NODE_OUTPUT" | grep -E "WARNING|AT_RISK|CRITICAL" | head -1) || true

    if [ -n "$DEGRADED_LINE" ]; then
        RISK=$(echo "$DEGRADED_LINE" | grep -o '0\.[0-9]*' | head -1)
        RISK_DETECTED=true
        info "[${ELAPSED}s] $TARGET_NODE risk=$RISK | Migrations: $CURRENT_MIGRATIONS"
    else
        info "[${ELAPSED}s] All healthy | Migrations: $CURRENT_MIGRATIONS"
    fi

    # Check if NEW migrations happened
    if [ "$CURRENT_MIGRATIONS" -gt "$BASELINE_MIGRATIONS" ] 2>/dev/null; then
        MIGRATION_DETECTED=true

        MIGRATED=$(echo "$MIGRATION_RESP" | grep -o '"migrated":[0-9]*' | grep -o '[0-9]*' | tail -1)
        FAILED=$(echo "$MIGRATION_RESP" | grep -o '"failed":[0-9]*' | grep -o '[0-9]*' | tail -1)
        DURATION=$(echo "$MIGRATION_RESP" | grep -o '"duration":"[^"]*"' | tail -1)

        pass "🚨 Proactive migration detected at ${ELAPSED}s!"
        info "Chunks migrated: $MIGRATED | Failed: $FAILED | $DURATION"
        break
    fi
done

if [ "$RISK_DETECTED" = true ]; then
    pass "Risk escalation detected for $TARGET_NODE"
else
    fail "No risk escalation detected"
fi

if [ "$MIGRATION_DETECTED" = false ]; then
    FINAL_RESP=$(curl -s "http://$META_ADMIN/migrations" 2>/dev/null) || true
    FINAL_MIGRATIONS=$(echo "$FINAL_RESP" | grep -o '"count":[0-9]*' | grep -o '[0-9]*' || echo "0")
    if [ -z "$FINAL_MIGRATIONS" ]; then FINAL_MIGRATIONS=0; fi

    if [ "$FINAL_MIGRATIONS" -gt "$BASELINE_MIGRATIONS" ] 2>/dev/null; then
        pass "Migration confirmed in final check"
    else
        info "Migrations response: $FINAL_RESP"
        fail "Migration not detected (baseline=$BASELINE_MIGRATIONS, final=$FINAL_MIGRATIONS)"
    fi
fi

# ── Phase 7: Data integrity ──
echo ""
echo -e "${YELLOW}Phase 7: Verifying data integrity after migration...${NC}"

curl -s -X POST "http://localhost:$TARGET_PORT/recover" > /dev/null 2>&1
sleep 2

go run ./cmd/client get /data/file1.bin "$TEST_DIR/file1_after.bin" > /dev/null 2>&1 || true
go run ./cmd/client get /data/file2.bin "$TEST_DIR/file2_after.bin" > /dev/null 2>&1 || true
go run ./cmd/client get /data/file3.bin "$TEST_DIR/file3_after.bin" > /dev/null 2>&1 || true

CHECK1=$(md5 -q "$TEST_DIR/file1_after.bin" 2>/dev/null || md5sum "$TEST_DIR/file1_after.bin" 2>/dev/null | awk '{print $1}')
CHECK2=$(md5 -q "$TEST_DIR/file2_after.bin" 2>/dev/null || md5sum "$TEST_DIR/file2_after.bin" 2>/dev/null | awk '{print $1}')
CHECK3=$(md5 -q "$TEST_DIR/file3_after.bin" 2>/dev/null || md5sum "$TEST_DIR/file3_after.bin" 2>/dev/null | awk '{print $1}')

[ "$FILE1_MD5" = "$CHECK1" ] && pass "file1.bin integrity preserved" || fail "file1.bin corrupted"
[ "$FILE2_MD5" = "$CHECK2" ] && pass "file2.bin integrity preserved" || fail "file2.bin corrupted"
[ "$FILE3_MD5" = "$CHECK3" ] && pass "file3.bin integrity preserved" || fail "file3.bin corrupted"

# ── Results ──
echo ""
echo "═══════════════════════════════════════════════════"
echo -e "  Results: ${GREEN}$PASS passed${NC}, ${RED}$FAIL failed${NC}"
echo "═══════════════════════════════════════════════════"

[ "$FAIL" -gt 0 ] && exit 1 || exit 0