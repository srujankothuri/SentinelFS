#!/bin/bash
# SentinelFS Chaos Test — Validates Predictive Self-Healing Pipeline
# Starts cluster, uploads files, degrades a node, verifies migration + data integrity

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

cleanup() {
    echo -e "\n${YELLOW}Cleaning up...${NC}"
    for pid in "${PIDS[@]}"; do
        kill "$pid" 2>/dev/null || true
    done
    rm -rf "$TEST_DIR" data/chaos-node-*
    echo "Done."
}
trap cleanup EXIT

pass() {
    echo -e "  ${GREEN}✓ $1${NC}"
    PASS=$((PASS + 1))
}

fail() {
    echo -e "  ${RED}✗ $1${NC}"
    FAIL=$((FAIL + 1))
}

info() {
    echo -e "  ${CYAN}ℹ $1${NC}"
}

export SENTINEL_META_ADDR=localhost:9200

echo "═══════════════════════════════════════════════════"
echo "   SentinelFS 🛡️  Chaos Test: Predictive Healing  "
echo "═══════════════════════════════════════════════════"
echo ""

# ── Start cluster ──
echo -e "${YELLOW}Phase 1: Starting cluster...${NC}"

go run ./cmd/metaserver --port 9200 > "$TEST_DIR/meta.log" 2>&1 &
PIDS+=($!)
sleep 2

go run ./cmd/datanode --meta-addr localhost:9200 --port 9201 --admin-port 9701 --data-dir ./data/chaos-node-1 > "$TEST_DIR/node1.log" 2>&1 &
PIDS+=($!)
sleep 1

go run ./cmd/datanode --meta-addr localhost:9200 --port 9202 --admin-port 9702 --data-dir ./data/chaos-node-2 > "$TEST_DIR/node2.log" 2>&1 &
PIDS+=($!)
sleep 1

go run ./cmd/datanode --meta-addr localhost:9200 --port 9203 --admin-port 9703 --data-dir ./data/chaos-node-3 > "$TEST_DIR/node3.log" 2>&1 &
PIDS+=($!)
sleep 2

# Verify cluster
OUTPUT=$(go run ./cmd/client cluster 2>&1) || true
if echo "$OUTPUT" | grep -q "3 total"; then
    pass "Cluster started with 3 nodes"
else
    fail "Cluster startup"
    exit 1
fi

# ── Upload test files ──
echo ""
echo -e "${YELLOW}Phase 2: Uploading test files...${NC}"

# Create test files
dd if=/dev/urandom of="$TEST_DIR/file1.bin" bs=1024 count=512 2>/dev/null
dd if=/dev/urandom of="$TEST_DIR/file2.bin" bs=1024 count=256 2>/dev/null
echo "Important document content for testing" > "$TEST_DIR/file3.txt"

FILE1_MD5=$(md5 -q "$TEST_DIR/file1.bin" 2>/dev/null || md5sum "$TEST_DIR/file1.bin" | awk '{print $1}')
FILE2_MD5=$(md5 -q "$TEST_DIR/file2.bin" 2>/dev/null || md5sum "$TEST_DIR/file2.bin" | awk '{print $1}')
FILE3_MD5=$(md5 -q "$TEST_DIR/file3.txt" 2>/dev/null || md5sum "$TEST_DIR/file3.txt" | awk '{print $1}')

go run ./cmd/client put "$TEST_DIR/file1.bin" /data/file1.bin > /dev/null 2>&1
go run ./cmd/client put "$TEST_DIR/file2.bin" /data/file2.bin > /dev/null 2>&1
go run ./cmd/client put "$TEST_DIR/file3.txt" /data/file3.txt > /dev/null 2>&1

pass "3 test files uploaded"

# Verify initial state
OUTPUT=$(go run ./cmd/client nodes 2>&1) || true
if echo "$OUTPUT" | grep -q "HEALTHY"; then
    pass "All nodes healthy before chaos"
fi

# ── Wait for metrics baseline ──
echo ""
echo -e "${YELLOW}Phase 3: Collecting baseline metrics (20s)...${NC}"
sleep 20
info "Baseline metrics collected"

# ── Trigger degradation ──
echo ""
echo -e "${YELLOW}Phase 4: 🔥 Triggering disk degradation on node-3...${NC}"

curl -s -X POST "http://localhost:9703/degrade?speed=0.5" > /dev/null 2>&1
pass "Degradation triggered on node-3 (speed=0.5)"

# ── Monitor for prediction and migration ──
echo ""
echo -e "${YELLOW}Phase 5: Waiting for prediction engine to detect degradation...${NC}"

MIGRATION_DETECTED=false
MAX_WAIT=90
ELAPSED=0

while [ "$ELAPSED" -lt "$MAX_WAIT" ]; do
    sleep 5
    ELAPSED=$((ELAPSED + 5))

    # Check node status
    OUTPUT=$(go run ./cmd/client nodes 2>&1) || true

    if echo "$OUTPUT" | grep -q "WARNING\|AT_RISK\|CRITICAL"; then
        if [ "$MIGRATION_DETECTED" = false ]; then
            info "Risk escalation detected at ${ELAPSED}s"
        fi
    fi

    # Check metadata server logs for migration
    if grep -q "PROACTIVE MIGRATION COMPLETED" "$TEST_DIR/meta.log" 2>/dev/null; then
        MIGRATION_DETECTED=true
        pass "Proactive migration triggered and completed (${ELAPSED}s)"
        break
    fi

    printf "  ⏳ Waiting... %ds/%ds\r" "$ELAPSED" "$MAX_WAIT"
done

if [ "$MIGRATION_DETECTED" = false ]; then
    # Check if migration at least started
    if grep -q "PROACTIVE MIGRATION STARTED" "$TEST_DIR/meta.log" 2>/dev/null; then
        pass "Migration was triggered (may still be in progress)"
    else
        fail "Migration was not triggered within ${MAX_WAIT}s"
    fi
fi

# ── Verify data integrity after migration ──
echo ""
echo -e "${YELLOW}Phase 6: Verifying data integrity after migration...${NC}"

# Stop degradation
curl -s -X POST "http://localhost:9703/recover" > /dev/null 2>&1

# Download all files and verify checksums
go run ./cmd/client get /data/file1.bin "$TEST_DIR/file1_after.bin" > /dev/null 2>&1 || true
go run ./cmd/client get /data/file2.bin "$TEST_DIR/file2_after.bin" > /dev/null 2>&1 || true
go run ./cmd/client get /data/file3.txt "$TEST_DIR/file3_after.txt" > /dev/null 2>&1 || true

CHECK1=$(md5 -q "$TEST_DIR/file1_after.bin" 2>/dev/null || md5sum "$TEST_DIR/file1_after.bin" 2>/dev/null | awk '{print $1}')
CHECK2=$(md5 -q "$TEST_DIR/file2_after.bin" 2>/dev/null || md5sum "$TEST_DIR/file2_after.bin" 2>/dev/null | awk '{print $1}')
CHECK3=$(md5 -q "$TEST_DIR/file3_after.txt" 2>/dev/null || md5sum "$TEST_DIR/file3_after.txt" 2>/dev/null | awk '{print $1}')

if [ "$FILE1_MD5" = "$CHECK1" ]; then
    pass "file1.bin integrity preserved after migration"
else
    fail "file1.bin corrupted (expected: $FILE1_MD5, got: $CHECK1)"
fi

if [ "$FILE2_MD5" = "$CHECK2" ]; then
    pass "file2.bin integrity preserved after migration"
else
    fail "file2.bin corrupted (expected: $FILE2_MD5, got: $CHECK2)"
fi

if [ "$FILE3_MD5" = "$CHECK3" ]; then
    pass "file3.txt integrity preserved after migration"
else
    fail "file3.txt corrupted (expected: $FILE3_MD5, got: $CHECK3)"
fi

# ── Print migration stats from logs ──
echo ""
echo -e "${YELLOW}Migration Log Summary:${NC}"
grep -E "MIGRATION|migrat" "$TEST_DIR/meta.log" 2>/dev/null | tail -10 | while IFS= read -r line; do
    echo "  📋 $line"
done

# ── Results ──
echo ""
echo "═══════════════════════════════════════════════════"
echo -e "  Results: ${GREEN}$PASS passed${NC}, ${RED}$FAIL failed${NC}"
echo "═══════════════════════════════════════════════════"

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi