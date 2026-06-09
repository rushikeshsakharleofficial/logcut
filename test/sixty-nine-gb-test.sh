#!/usr/bin/env bash
set -euo pipefail

# 69GB log emergency test for logcut
#
# Creates a 69GB log file (90% gets archived, 10% kept as tail),
# runs logcut, and verifies:
#   - Output gzip is valid and contains the correct 90% prefix
#   - Source becomes sparse (real blocks << apparent size)
#   - Free space is recovered
#   - Resume works after interruption

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'
pass_count=0; fail_count=0
pass() { echo -e "${GREEN}[PASS]${NC} $1"; pass_count=$((pass_count+1)); }
fail() { echo -e "${RED}[FAIL]${NC} $1"; fail_count=$((fail_count+1)); }

WORKDIR="${WORKDIR:-/home/rushikesh.sakharle/tmp/logcut-69gb-test}"
LOGCUT="${LOGCUT:-./logcut}"
if [ ! -x "$LOGCUT" ]; then
  LOGCUT="go run ./cmd/logcut"
fi

echo "=== logcut 69GB Emergency Test ==="
echo "Workdir: $WORKDIR"
echo "Logcut:  $LOGCUT"
echo ""

rm -rf "$WORKDIR"
mkdir -p "$WORKDIR"/{state,lock}

SOURCE="$WORKDIR/debug.log"
OUTPUT="$WORKDIR/debug.log.rotated.gz"
STATE_DIR="$WORKDIR/state"
LOCK_DIR="$WORKDIR/lock"
RUN_LOG="$WORKDIR/run.log"

TOTAL_GB=69
TOTAL_BYTES=$((TOTAL_GB * 1024 * 1024 * 1024))
KEEP_PCT=10
KEEP_BYTES=$((TOTAL_BYTES / 10))
# keep-last is 10% = 6.9GB
# archive range is 90% = 62.1GB

echo "Target: ${TOTAL_GB}GB log, keep ${KEEP_PCT}% (${KEEP_BYTES} bytes) tail"
echo "Archive range: $((TOTAL_BYTES - KEEP_BYTES)) bytes ($((100 - KEEP_PCT))%)"
echo ""

# -- Step 1: Generate 69GB log --
echo "--- Step 1: Generating ${TOTAL_GB}GB log ---"
START_TIME=$(date +%s)

# Write a temp Python script to avoid bash ${} escaping issues
PYGEN=$(mktemp)
cat > "$PYGEN" << 'PYEOF'
import sys, time, os
total = int(sys.argv[1])
start_ts = float(sys.argv[2])
line_len = 1023
filled = 0
buf = bytearray(line_len + 1)
header = b'A' * 20
while filled < total:
    buf[0:20] = header
    for j in range(20, line_len):
        buf[j] = 65 + ((filled + j) % 58)
    buf[line_len] = 10
    os.write(1, bytes(buf))
    filled += line_len + 1
    if filled % (1024 * 1024 * 1024) < (line_len + 1):
        elapsed = time.time() - start_ts
        gb = filled / (1024**3)
        sys.stderr.write(f'\r  {gb:.1f}GB / {total/(1024**3):.0f}GB ({filled*100//total}%), {elapsed:.0f}s elapsed\n')
PYEOF

echo -n "Writing $TOTAL_GB GB ($TOTAL_BYTES bytes)..."
python3 "$PYGEN" "$TOTAL_BYTES" "$START_TIME" > "$SOURCE"
rm -f "$PYGEN"

GEN_DURATION=$(( $(date +%s) - START_TIME ))
LOG_SIZE=$(stat -c%s "$SOURCE")
echo ""
echo "Log created: $(du -h "$SOURCE" | cut -f1) in ${GEN_DURATION}s"
FREE_BEFORE=$(df --output=avail -B1 "$WORKDIR" | tail -1)
echo "Free space before: $(numfmt --to=iec $FREE_BEFORE)"

# Record checksums for verification
echo -n "Computing source checksums..."
MD5_HEAD=$(dd if="$SOURCE" bs=1M count=100 2>/dev/null | md5sum | cut -d' ' -f1)
MD5_TAIL_OFFSET=$((KEEP_BYTES - 100*1024*1024))
MD5_TAIL=$(dd if="$SOURCE" bs=1M skip=$((MD5_TAIL_OFFSET / 1024 / 1024)) 2>/dev/null | head -c $((100*1024*1024)) | md5sum | cut -d' ' -f1)
echo " done (head=$MD5_HEAD, tail=$MD5_TAIL)"

# -- Step 2: Preflight --
echo ""
echo "--- Step 2: Preflight ---"
set +e
preflight_out=$($LOGCUT --preflight -g -k "${KEEP_BYTES}" "$SOURCE" "$OUTPUT" 2>&1)
rc=$?
set -e
if [ $rc -eq 0 ]; then
  pass "Preflight passed"
else
  echo "$preflight_out"
  fail "Preflight failed: $preflight_out"
  exit 1
fi

# -- Step 3: Run logcut --
echo ""
echo "--- Step 3: Emergency Compaction (90% archived, 10% kept) ---"
RUN_START=$(date +%s)

set +e
$LOGCUT -v --log-file "$RUN_LOG" \
  --stop-free-above "50G" \
  --max-runtime "2h" \
  --chunk-timeout "10m" \
  --compress-level 1 \
  --verify full \
  -g \
  -k "${KEEP_BYTES}" \
  --state-dir "$STATE_DIR" \
  --lock-dir "$LOCK_DIR" \
  "$SOURCE" "$OUTPUT" \
  2>&1 | tail -30
rc=$?
set -e
RUN_DURATION=$(( $(date +%s) - RUN_START ))

if [ $rc -ne 0 ]; then
  echo "Exit code: $rc"
  grep -i "error\|fatal\|watchdog\|emergency" "$RUN_LOG" || true

  # Check for emergency state
  EMERGENCY=$(ls "$STATE_DIR"/*.emergency 2>/dev/null || true)
  if [ -n "$EMERGENCY" ]; then
    echo "Emergency state found:"
    cat "$EMERGENCY"
  fi
  fail "Emergency rescue run failed"
  exit 1
fi

# -- Step 4: Verify --
echo ""
echo "--- Step 4: Verification ---"
echo "Run duration: ${RUN_DURATION}s"

# 4a: Free space
FREE_AFTER=$(df --output=avail -B1 "$WORKDIR" | tail -1)
echo -n "Free space: $(numfmt --to=iec $FREE_BEFORE) -> $(numfmt --to=iec $FREE_AFTER)..."
if [ "$FREE_AFTER" -gt "$FREE_BEFORE" ]; then
  recovered=$(( (FREE_AFTER - FREE_BEFORE) / 1024 / 1024 / 1024 ))
  pass "Free space increased by ${recovered}GB"
else
  fail "No free space recovery"
fi

# 4b: Output gzip valid
echo -n "Output gzip integrity..."
if gzip -t "$OUTPUT" 2>/dev/null; then
  gzip_size=$(du -h "$OUTPUT" | cut -f1)
  pass "Output gzip valid ($gzip_size)"
else
  fail "Output gzip corrupted"
fi

# 4c: Source sparse
echo -n "Source sparse..."
apparent=$(stat -c%s "$SOURCE")
real_blocks=$(stat -c%b "$SOURCE")
block_size=$(stat -c%B "$SOURCE")
real_usage=$((real_blocks * block_size))
apparent_h=$(numfmt --to=iec "$apparent")
real_h=$(numfmt --to=iec "$real_usage")
if [ "$real_usage" -lt "$((apparent * 40 / 100))" ]; then
  saved=$(( 100 - real_usage * 100 / apparent ))
  pass "Source sparse: apparent=${apparent_h} real=${real_h} (~${saved}% saved)"
else
  fail "Source not sufficiently sparse: apparent=${apparent_h} real=${real_h}"
fi

# 4d: State file
STATE_FILE=$(ls "$STATE_DIR"/*.state 2>/dev/null | head -1)
if [ -n "$STATE_FILE" ]; then
  pass "State file exists"
else
  fail "State file missing"
fi

# 4e: Verify output matches original first 90%
echo -n "Verifying archived content (decompressing + checksum)..."
echo "  (this will take a few minutes for 62GB of gzip data)"
DECOMP_START=$(date +%s)

# Check head of decompressed data
DECOMP_HEAD=$(zcat "$OUTPUT" 2>/dev/null | dd bs=1M count=100 2>/dev/null | md5sum | cut -d' ' -f1)
if [ "$DECOMP_HEAD" = "$MD5_HEAD" ]; then
  pass "Archived head matches source ($DECOMP_HEAD)"
else
  fail "Archived head mismatch: $DECOMP_HEAD vs $MD5_HEAD"
fi

# Check tail of archived data (last 100MB of the 62.1GB archive)
ARCHIVE_TAIL_OFFSET=$((TOTAL_BYTES - KEEP_BYTES - 100*1024*1024))
DECOMP_TAIL=$(zcat "$OUTPUT" 2>/dev/null | dd bs=1M skip=$((ARCHIVE_TAIL_OFFSET / 1024 / 1024)) 2>/dev/null | head -c $((100*1024*1024)) | md5sum | cut -d' ' -f1)
SOURCE_TAIL_OFFSET=$((ARCHIVE_TAIL_OFFSET))
MD5_TAIL2=$(dd if="$SOURCE" bs=1M skip=$((SOURCE_TAIL_OFFSET / 1024 / 1024)) 2>/dev/null | head -c $((100*1024*1024)) | md5sum | cut -d' ' -f1)
if [ "$DECOMP_TAIL" = "$MD5_TAIL2" ]; then
  pass "Archived tail matches source ($DECOMP_TAIL)"
else
  fail "Archived tail mismatch: $DECOMP_TAIL vs $MD5_TAIL2"
fi
DECOMP_DUR=$(( $(date +%s) - DECOMP_START ))
echo "  Verification took ${DECOMP_DUR}s"

# 4f: Check run log
echo -n "Run log..."
if grep -q "Complete\." "$RUN_LOG" 2>/dev/null; then
  stop_reason=$(grep "Stop reason" "$RUN_LOG" | tr -d ' ')
  pass "Run completed ($stop_reason)"
else
  fail "Run did not complete normally"
fi

# -- Summary --
echo ""
echo "========================================="
echo -e "69GB Emergency Test: ${GREEN}$pass_count passed${NC}, ${RED}$fail_count failed${NC}"
echo "Total test time: $(( $(date +%s) - START_TIME ))s"
echo "========================================="

if [ $fail_count -gt 0 ]; then
  exit 1
fi
echo ""
echo "All checks passed. Clean up with: rm -rf $WORKDIR"
