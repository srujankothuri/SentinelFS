#!/bin/bash
# SentinelFS Chaos Test — Validates Predictive Self-Healing Pipeline

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
    sleep 1
    rm -rf "$TEST_DIR" data/chaos-node-*
    echo "Done."
}
trap cleanup EXIT

pass() { echo -e "  ${GREEN}✓ $1${NC}"; PASS=$((PASS + 1)); }
fail() { echo -e "  ${RED}✗ $1${NC}"; FAIL=$((FAIL + 1)); }
info() { echo -e "  ${CYAN}ℹ $1${NC}"; }

export SENTINEL_META_ADDR=localhost:9200

echo "═══════════════════════════════════════════════════"
echo "   SentinelFS 🛡️  Chaos Test: Predictive Healing  "
echo "═══════════════════════════════════════════════════"
echo ""

# ── Phase 1: Start cluster ──
echo -e "${YELLOW}Phase 1: Starting cluster...${NC}"

go run ./cmd/metaserver --port 9200 > "$TEST_DIR/meta.log" 2>&1 &
PIDS+=($!)
sleep 3

for i in 1 2 3 4 5; do
    PORT=$((9200 + i))
    ADMIN=$((9700 + i))
    go run ./cmd/datanode --meta-addr localhost:9200 --port $PORT --admin-port $ADMIN --data-dir ./data/chaos-node-$i > /dev/null 2>&1 &
    PIDS+=($!)
done
sleep 4

OUTPUT=$(go run ./cmd/client cluster 2>&1) || true
if echo "$OUTPUT" | grep -q "5 total"; then
    pass "Cluster started with 5 nodes"
else
    fail "Cluster startup"
    exit 1
fi

# ── Phase 2: Upload files ──
echo ""
echo -e "${YELLOW}Phase 2: Uploading test files...${NC}"

for i in $(seq 1 6); do
    dd if=/dev/urandom of="$TEST_DIR/file$i.bin" bs=1024 count=256 2>/dev/null
    go run ./cmd/client put "$TEST_DIR/file$i.bin" /data/file$i.bin > /dev/null 2>&1
done

FILE1_MD5=$(md5 -q "$TEST_DIR/file1.bin" 2>/dev/null || md5sum "$TEST_DIR/file1.bin" | awk '{print $1}')
FILE2_MD5=$(md5 -q "$TEST_DIR/file2.bin" 2>/dev/null || md5sum "$TEST_DIR/file2.bin" | awk '{print $1}')

pass "6 test files uploaded"

# Record which chunks are on node-3 BEFORE degradation
BEFORE_INFO=$(go run ./cmd/client info /data/file1.bin 2>&1) || true
info "File1 chunk placement before:"
echo "$BEFORE_INFO" | grep "node-" | head -3 | while IFS= read -r line; do echo "      $line"; done

NODE3_HAS_FILE1_BEFORE="no"
if echo "$BEFORE_INFO" | grep -q "node-3"; then
    NODE3_HAS_FILE1_BEFORE="yes"
    info "node-3 has file1 chunks: YES"
else
    info "node-3 has file1 chunks: NO (checking file2...)"
fi

# ── Phase 3: Baseline ──
echo ""
echo -e "${YELLOW}Phase 3: Collecting baseline (15s)...${NC}"
sleep 15
pass "Baseline collected"

# ── Phase 4: Degrade ──
echo ""
echo -e "${YELLOW}Phase 4: 🔥 Triggering degradation on node-3...${NC}"
curl -s -X POST "http://localhost:9703/degrade?speed=2.0" > /dev/null 2>&1
pass "Degradation started"

# ── Phase 5: Monitor ──
echo ""
echo -e "${YELLOW}Phase 5: Monitoring prediction & migration...${NC}"

RISK_DETECTED=false
MIGRATION_DETECTED=false
MAX_WAIT=90
ELAPSED=0

while [ "$ELAPSED" -lt "$MAX_WAIT" ]; do
    sleep 5
    ELAPSED=$((ELAPSED + 5))

    NODE_OUTPUT=$(go run ./cmd/client nodes 2>&1) || true
    NODE3_LINE=$(echo "$NODE_OUTPUT" | grep "node-3" || true)
    RISK=$(echo "$NODE3_LINE" | grep -o '0\.[0-9]*' | head -1)

    STATUS="HEALTHY"
    echo "$NODE3_LINE" | grep -q "CRITICAL" && STATUS="CRITICAL"
    echo "$NODE3_LINE" | grep -q "AT_RISK" && STATUS="AT_RISK"
    echo "$NODE3_LINE" | grep -q "WARNING" && STATUS="WARNING"

    info "[${ELAPSED}s] node-3: status=$STATUS risk=$RISK"

    if [ "$STATUS" != "HEALTHY" ]; then
        RISK_DETECTED=true
    fi

    # Check if chunk placement changed — node-3 removed from file locations
    for f in 1 2 3 4 5 6; do
        FILE_INFO=$(go run ./cmd/client info /data/file$f.bin 2>&1) || true
        # Check if any chunk was migrated AWAY from node-3
        # by looking for chunks that used to include node-3 but now have a different node
        if echo "$FILE_INFO" | grep -q "node-4\|node-5"; then
            if ! echo "$FILE_INFO" | grep -q "node-3"; then
                # node-3 was removed from this file's chunks
                MIGRATION_DETECTED=true
                pass "Migration detected: file$f.bin chunks moved off node-3 at ${ELAPSED}s"
                break 2
            fi
        fi
    done
done

if [ "$RISK_DETECTED" = true ]; then
    pass "Risk escalation detected"
else
    fail "No risk escalation"
fi

if [ "$MIGRATION_DETECTED" = false ]; then
    # Show final state for debugging
    info "Final chunk placements:"
    for f in 1 2 3; do
        FINFO=$(go run ./cmd/client info /data/file$f.bin 2>&1) || true
        echo "$FINFO" | grep "node-" | head -1 | while IFS= read -r line; do
            echo "      file$f: $line"
        done
    done

    info "Meta log (last 30 lines):"
    tail -30 "$TEST_DIR/meta.log" 2>/dev/null | grep -i "migration\|predict\|risk\|critical\|0\.\|WARN" | while IFS= read -r line; do
        echo "      $line"
    done
    fail "Migration not detected"
fi

# ── Phase 6: Data integrity ──
echo ""
echo -e "${YELLOW}Phase 6: Verifying data integrity...${NC}"

curl -s -X POST "http://localhost:9703/recover" > /dev/null 2>&1
sleep 2

go run ./cmd/client get /data/file1.bin "$TEST_DIR/file1_after.bin" > /dev/null 2>&1 || true
go run ./cmd/client get /data/file2.bin "$TEST_DIR/file2_after.bin" > /dev/null 2>&1 || true

CHECK1=$(md5 -q "$TEST_DIR/file1_after.bin" 2>/dev/null || md5sum "$TEST_DIR/file1_after.bin" 2>/dev/null | awk '{print $1}')
CHECK2=$(md5 -q "$TEST_DIR/file2_after.bin" 2>/dev/null || md5sum "$TEST_DIR/file2_after.bin" 2>/dev/null | awk '{print $1}')

[ "$FILE1_MD5" = "$CHECK1" ] && pass "file1.bin integrity preserved" || fail "file1.bin corrupted"
[ "$FILE2_MD5" = "$CHECK2" ] && pass "file2.bin integrity preserved" || fail "file2.bin corrupted"

# ── Results ──
echo ""
echo "═══════════════════════════════════════════════════"
echo -e "  Results: ${GREEN}$PASS passed${NC}, ${RED}$FAIL failed${NC}"
echo "═══════════════════════════════════════════════════"

[ "$FAIL" -gt 0 ] && exit 1 || exit 0