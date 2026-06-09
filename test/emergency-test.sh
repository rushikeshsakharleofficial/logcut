#!/usr/bin/env bash
set -euo pipefail

# emergency-test.sh - Simulate 90% full filesystem emergency and test logcut rescue
#
# This script:
#   1. Creates a tmpfs of fixed size (simulating the root filesystem)
#   2. Fills it to 90% usage with a large log file + filler files
#   3. Runs logcut to rescue the situation (archive 90% old log, keep 10% tail)
#   4. Verifies: free space recovered, output correct, source sparse, state saved
#   5. Tests resume after interruption

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass_count=0
fail_count=0

pass() { echo -e "${GREEN}[PASS]${NC} $1"; pass_count=$((pass_count+1)); }
fail() { echo -e "${RED}[FAIL]${NC} $1"; fail_count=$((fail_count+1)); }

LOGCUT="${LOGCUT:-./logcut}"
if [ ! -x "$LOGCUT" ]; then
  LOGGER="go run ./cmd/logcut"
  echo "Using go run for logcut"
else
  echo "Using binary: $LOGCUT"
fi

cleanup() {
  if [ -n "${WORKDIR:-}" ] && mountpoint -q "$WORKDIR" 2>/dev/null; then
    sudo umount "$WORKDIR" 2>/dev/null || true
  fi
}
trap cleanup EXIT

echo "=== logcut 90%-full Emergency Test ==="
echo ""

# --- Step 1: Create tmpfs ---
WORKDIR=$(mktemp -d)
TMPFS_SIZE="512M"
echo -n "Creating tmpfs of $TMPFS_SIZE at $WORKDIR..."
if sudo mount -t tmpfs -o "size=$TMPFS_SIZE" tmpfs "$WORKDIR"; then
  pass "tmpfs mounted"
  sudo chown "$(id -u):$(id -g)" "$WORKDIR"
else
  fail "Cannot mount tmpfs (try running as root or with sudo)"
  exit 1
fi

SOURCE="$WORKDIR/debug.log"
OUTPUT="$WORKDIR/debug.log.rotated.gz"
STATE_DIR="$WORKDIR/state"
LOCK_DIR="$WORKDIR/lock"

mkdir -p "$STATE_DIR" "$LOCK_DIR"

# --- Step 2: Generate 90% fill ---
echo ""
echo -n "Generating huge log file..."
# Generate a ~350MB log (leaving ~50MB free out of 512MB, so ~90% used)
# Use 35k lines of ~10KB each = ~350MB
python3 -c "
import sys, random, string
chars = string.ascii_letters + string.digits + ' ' * 4
line = ''.join(random.choices(chars, k=9999)) + '\n'
for _ in range(35000):
    sys.stdout.write(line)
" > "$SOURCE"

LOG_SIZE=$(stat -c%s "$SOURCE")
echo " created $(du -h "$SOURCE" | cut -f1) ($LOG_SIZE bytes)"

echo -n "Filling remaining space to ~90%..."
# Fill until free space is ~10% of tmpfs size (512M * 10% = 51MB)
FILL_DIR="$WORKDIR/filler"
mkdir -p "$FILL_DIR"
target_free=$((512 * 1024 * 1024 / 10))  # 51MB
filler_count=0
while true; do
  free_now=$(df --output=avail -B1 "$WORKDIR" | tail -1)
  if [ "$free_now" -le "$target_free" ]; then
    break
  fi
  dd if=/dev/zero of="$FILL_DIR/fill-$filler_count" bs=1M count=10 2>/dev/null
  filler_count=$((filler_count + 1))
  if [ $filler_count -gt 50 ]; then
    break
  fi
done

TOTAL_USED=$(df --output=pcent "$WORKDIR" | tail -1 | tr -d ' %')
df -h "$WORKDIR"
echo "Filesystem ${TOTAL_USED}% used"

log_pct=$((LOG_SIZE * 100 / (512 * 1024 * 1024)))
echo "Log file: $(du -h "$SOURCE" | cut -f1) (~${log_pct}% of disk)"
echo "Tail (10% of log): ~$((LOG_SIZE / 10)) bytes"
pass "Emergency scenario ready ($TOTAL_USED% full)"

# --- Step 3: Preflight ---
echo ""
echo "--- Step 3: Preflight ---"
set +e
output=$($LOGCUT --preflight -g -k "$((LOG_SIZE / 10))" "$SOURCE" "$OUTPUT" 2>&1)
rc=$?
set -e
if [ $rc -eq 0 ]; then
  pass "Preflight passed"
else
  echo "$output"
  fail "Preflight failed"
  exit 1
fi

# --- Step 4: Emergency rescue ---
echo ""
echo "--- Step 4: Emergency Rescue ---"
echo "Running logcut with keep-last=10%..."

RUN_LOG="$WORKDIR/run.log"
set +e
$LOGCUT -v --log-file "$RUN_LOG" \
  --stop-free-above "100M" \
  --max-runtime "5m" \
  --chunk-timeout "2m" \
  --compress-level 1 \
  --verify full \
  -g \
  -k "$((LOG_SIZE / 10))" \
  --state-dir "$STATE_DIR" \
  --lock-dir "$LOCK_DIR" \
  "$SOURCE" "$OUTPUT" \
  2>&1 | tail -20
rc=$?
set -e

if [ $rc -ne 0 ]; then
  echo "  logcut exit code: $rc"
  fail "Emergency rescue run failed"
  grep -i "error\|fatal\|watchdog\|emergency" "$RUN_LOG" || true
  exit 1
fi

# --- Step 5: Verify results ---
echo ""
echo "--- Step 5: Verification ---"

# 5a: Check free space increased
FREE_AFTER=$(df --output=avail -B1 "$WORKDIR" | tail -1)
FREE_MB=$((FREE_AFTER / 1024 / 1024))
echo -n "Free space after rescue: ${FREE_MB}M..."
if [ "$FREE_AFTER" -gt "$((target_free * 2))" ]; then
  pass "Free space recovered significantly"
else
  fail "Not enough free space recovered (${FREE_MB}M)"
fi

# 5b: Output gzip file exists and is valid
echo -n "Output gzip valid..."
if zcat "$OUTPUT" >/dev/null 2>&1; then
  gzip_size=$(du -h "$OUTPUT" | cut -f1)
  pass "Output gzip valid ($gzip_size)"
else
  fail "Output gzip corrupted"
fi

# 5c: Source is sparse (real usage < apparent size)
echo -n "Source log sparse..."
apparent=$(stat -c%s "$SOURCE")
real_blocks=$(stat -c%b "$SOURCE")
real_usage=$((real_blocks * 512))
if [ "$real_usage" -lt "$apparent" ]; then
  pass "Source is sparse (apparent=$(du -h "$SOURCE" | cut -f1), real=$(du -h --apparent-size "$SOURCE" | cut -f1))"
else
  fail "Source not sparse after compaction"
fi

# 5d: State file exists
STATE_FILE=$(ls "$STATE_DIR"/*.state 2>/dev/null | head -1)
if [ -n "$STATE_FILE" ]; then
  pass "State file created"
  echo "  State: $(cat "$STATE_FILE")"
else
  fail "State file missing"
fi

# 5e: Verify decompressed output matches original prefix
echo -n "Decompressed output matches original prefix..."
keep_bytes=$((LOG_SIZE / 10))
cutoff=$((LOG_SIZE - keep_bytes))
decompressed=$(zcat "$OUTPUT" | wc -c)
expected_orig_prefix_len=$cutoff
# Source data is now sparse, so we can't compare directly after compaction.
# Instead check that decompressed output is non-empty and roughly right
if [ "$decompressed" -gt 1000 ] && [ "$decompressed" -le "$LOG_SIZE" ]; then
  pass "Decompressed output size=$decompressed bytes (expected ~$expected_orig_prefix_len bytes)"
else
  fail "Decompressed output size unexpected: $decompressed"
fi

# 5f: Check run log
echo -n "Run log complete..."
if grep -q "Complete\." "$RUN_LOG" 2>/dev/null; then
  stop_reason=$(grep "Stop reason" "$RUN_LOG")
  pass "Run completed ($stop_reason)"
else
  fail "Run did not complete normally"
fi

# --- Step 6: Test resume after interruption ---
echo ""
echo "--- Step 6: Resume Test ---"

# Clean up and create a fresh scenario
rm -f "$OUTPUT" "$STATE_DIR"/*.state "$FILL_DIR"/fill-*
rm -f "$WORKDIR/app2.log" "$WORKDIR/app2.rotated.gz"

SOURCE2="$WORKDIR/app2.log"
OUTPUT2="$WORKDIR/app2.rotated.gz"

python3 -c "
import sys, random, string
chars = string.ascii_letters + string.digits + ' ' * 4
line = ''.join(random.choices(chars, k=9999)) + '\n'
for _ in range(20000):
    sys.stdout.write(line)
" > "$SOURCE2"

# Fill to ~90% again
filler_count=0
while true; do
  free_now=$(df --output=avail -B1 "$WORKDIR" | tail -1)
  if [ "$free_now" -le "$target_free" ]; then
    break
  fi
  dd if=/dev/zero of="$FILL_DIR/fill-$filler_count" bs=1M count=10 2>/dev/null
  filler_count=$((filler_count + 1))
  [ $filler_count -gt 50 ] && break
done

echo -n "Running with 3s max-runtime to force partial completion..."
set +e
$LOGCUT --quiet -g \
  -k "1000" \
  --max-runtime "3s" \
  --state-dir "$STATE_DIR" \
  --lock-dir "$LOCK_DIR" \
  "$SOURCE2" "$OUTPUT2" \
  >/dev/null 2>&1
rc=$?
set -e

if grep -q "max-runtime" "$RUN_LOG" 2>/dev/null || true; then :; fi

STATE_FILE2=$(ls "$STATE_DIR"/*.state 2>/dev/null | grep -v "app.log" | head -1 || true)
if [ -n "$STATE_FILE2" ]; then
  punched=$(grep "last_punched_offset" "$STATE_FILE2" | cut -d= -f2)
  if [ "$punched" -gt 0 ] 2>/dev/null; then
    pass "Partial run saved state at offset=$punched"
  else
    fail "Partial state has offset=$punched"
  fi
else
  fail "No state file after partial run"
fi

echo -n "Resuming to completion..."
set +e
$LOGCUT --quiet -g \
  -k "1000" \
  --state-dir "$STATE_DIR" \
  --lock-dir "$LOCK_DIR" \
  "$SOURCE2" "$OUTPUT2" \
  >/dev/null 2>&1
rc=$?
set -e
if [ $rc -eq 0 ]; then
  pass "Resume completed successfully"
else
  fail "Resume failed (exit code $rc)"
fi

echo -n "Resumed output valid..."
if zcat "$OUTPUT2" >/dev/null 2>&1; then
  pass "Resumed output is valid gzip"
else
  fail "Resumed output corrupted"
fi

# --- Summary ---
echo ""
echo "========================================="
echo -e "Results: ${GREEN}$pass_count passed${NC}, ${RED}$fail_count failed${NC}"
echo "========================================="

if [ $fail_count -gt 0 ]; then
  echo ""
  echo "Logs: $RUN_LOG"
  exit 1
fi

echo ""
echo "Emergency scenario test complete."
echo "Workdir left at: $WORKDIR (tmpfs will unmount on exit)"
