#!/bin/bash
# SentinelFS Integration Test
# Starts a cluster, uploads/downloads files, and verifies integrity

set -e

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
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
    rm -rf "$TEST_DIR" data/test-node-*
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

echo "═══════════════════════════════════════"
echo "   SentinelFS Integration Tests"
echo "═══════════════════════════════════════"
echo ""

# ── Start cluster ──
echo -e "${YELLOW}Starting cluster...${NC}"

go run ./cmd/metaserver --port 9100 --admin-port 9610 &
PIDS+=($!)
sleep 2

go run ./cmd/datanode --meta-addr localhost:9100 --port 9101 --admin-port 9601 --data-dir ./data/test-node-1 &
PIDS+=($!)
sleep 1

go run ./cmd/datanode --meta-addr localhost:9100 --port 9102 --admin-port 9602 --data-dir ./data/test-node-2 &
PIDS+=($!)
sleep 1

go run ./cmd/datanode --meta-addr localhost:9100 --port 9103 --admin-port 9603 --data-dir ./data/test-node-3 &
PIDS+=($!)
sleep 2

echo ""

# ── Test 1: Cluster health ──
echo "Test 1: Cluster health check"
export SENTINEL_META_ADDR=localhost:9100
OUTPUT=$(go run ./cmd/client cluster 2>&1) || true
if echo "$OUTPUT" | grep -q "3 total"; then
    pass "Cluster shows 3 nodes"
else
    fail "Cluster health check (got: $OUTPUT)"
fi

# ── Test 2: Upload small file ──
echo "Test 2: Upload small file"
echo "Hello SentinelFS! Integration test content." > "$TEST_DIR/small.txt"
OUTPUT=$(SENTINEL_META_ADDR=localhost:9100 go run ./cmd/client put "$TEST_DIR/small.txt" /test/small.txt 2>&1) || true
if echo "$OUTPUT" | grep -q "Uploaded"; then
    pass "Small file uploaded"
else
    fail "Small file upload (got: $OUTPUT)"
fi

# ── Test 3: Download and verify ──
echo "Test 3: Download and verify integrity"
SENTINEL_META_ADDR=localhost:9100 go run ./cmd/client get /test/small.txt "$TEST_DIR/downloaded.txt" 2>&1 || true
if [ -f "$TEST_DIR/downloaded.txt" ]; then
    ORIG=$(md5 -q "$TEST_DIR/small.txt" 2>/dev/null || md5sum "$TEST_DIR/small.txt" | awk '{print $1}')
    DOWN=$(md5 -q "$TEST_DIR/downloaded.txt" 2>/dev/null || md5sum "$TEST_DIR/downloaded.txt" | awk '{print $1}')
    if [ "$ORIG" = "$DOWN" ]; then
        pass "File integrity verified (checksums match)"
    else
        fail "Checksum mismatch: $ORIG vs $DOWN"
    fi
else
    fail "Downloaded file not found"
fi

# ── Test 4: List files ──
echo "Test 4: List files"
OUTPUT=$(SENTINEL_META_ADDR=localhost:9100 go run ./cmd/client ls / 2>&1) || true
if echo "$OUTPUT" | grep -q "test"; then
    pass "File listing works"
else
    fail "File listing (got: $OUTPUT)"
fi

# ── Test 5: File info ──
echo "Test 5: File info"
OUTPUT=$(SENTINEL_META_ADDR=localhost:9100 go run ./cmd/client info /test/small.txt 2>&1) || true
if echo "$OUTPUT" | grep -q "Chunk"; then
    pass "File info shows chunk details"
else
    fail "File info (got: $OUTPUT)"
fi

# ── Test 6: Upload larger file ──
echo "Test 6: Upload larger file (1MB)"
dd if=/dev/urandom of="$TEST_DIR/large.bin" bs=1024 count=1024 2>/dev/null
OUTPUT=$(SENTINEL_META_ADDR=localhost:9100 go run ./cmd/client put "$TEST_DIR/large.bin" /test/large.bin 2>&1) || true
if echo "$OUTPUT" | grep -q "Uploaded"; then
    pass "Large file uploaded"
else
    fail "Large file upload (got: $OUTPUT)"
fi

# ── Test 7: Download large file and verify ──
echo "Test 7: Download large file and verify"
SENTINEL_META_ADDR=localhost:9100 go run ./cmd/client get /test/large.bin "$TEST_DIR/large_down.bin" 2>&1 || true
if [ -f "$TEST_DIR/large_down.bin" ]; then
    ORIG=$(md5 -q "$TEST_DIR/large.bin" 2>/dev/null || md5sum "$TEST_DIR/large.bin" | awk '{print $1}')
    DOWN=$(md5 -q "$TEST_DIR/large_down.bin" 2>/dev/null || md5sum "$TEST_DIR/large_down.bin" | awk '{print $1}')
    if [ "$ORIG" = "$DOWN" ]; then
        pass "Large file integrity verified"
    else
        fail "Large file checksum mismatch"
    fi
else
    fail "Large downloaded file not found"
fi

# ── Test 8: Delete file ──
echo "Test 8: Delete file"
OUTPUT=$(SENTINEL_META_ADDR=localhost:9100 go run ./cmd/client rm /test/small.txt 2>&1) || true
if echo "$OUTPUT" | grep -q "Deleted"; then
    pass "File deleted"
else
    fail "File delete (got: $OUTPUT)"
fi

# ── Test 9: Node info ──
echo "Test 9: Node health info"
OUTPUT=$(SENTINEL_META_ADDR=localhost:9100 go run ./cmd/client nodes 2>&1) || true
if echo "$OUTPUT" | grep -q "HEALTHY"; then
    pass "Node health info shows HEALTHY nodes"
else
    fail "Node health (got: $OUTPUT)"
fi

# ── Results ──
echo ""
echo "═══════════════════════════════════════"
echo -e "  Results: ${GREEN}$PASS passed${NC}, ${RED}$FAIL failed${NC}"
echo "═══════════════════════════════════════"

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi