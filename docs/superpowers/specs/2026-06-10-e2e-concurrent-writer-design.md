# E2E Concurrent Writer Tests — Design Spec
Date: 2026-06-10

## Goal

Add end-to-end Go tests that simulate the real-life logcut scenario: an application actively appending to a log file while logcut archives and punches the old portion. Four tests at two scales.

## Files

| File | Purpose |
|------|---------|
| `internal/cli/concurrent_test.go` | Tests A, B, C — 500 MB scale, runs in normal `go test` |
| `internal/cli/disaster_test.go` | Test D — fills 90% of actual disk, skipped in `-short` |

## Shared Helper — `startConcurrentWriter`

```
func startConcurrentWriter(t *testing.T, path string, bytesPerSec int64) func() []byte
```

- Opens file with `O_WRONLY|O_APPEND` — appends only, never seeks
- Writes lines of the form: `[timestamp] writer-<id> seq=<n> payload...\n`
- Throttles via `time.Sleep` when `bytesPerSec > 0`; unbounded when 0
- `stop()` signals the goroutine, waits for it to drain, returns all bytes written
- `t.Cleanup` registered so goroutine always stops even on test failure

## Test A — `TestIntegrationConcurrentWriterStability`

**Scale:** 500 MB initial log, writer at 1 MB/s  
**Logcut:** keep 10% (archive 90%), gzip, verify full

**Assertions:**
- `Run()` returns 0
- Output gzip decompresses without error
- Output is non-empty
- Source `BlocksUsed < Size` (sparse)
- Writer's last line is present in source

## Test B — `TestIntegrationConcurrentWriterIntegrity`

**Scale:** 500 MB initial log, writer at 256 KB/s  
**Logcut:** keep 10%, gzip, verify full

**Assertions:**
- `Run()` returns 0
- Snapshot `origData` and `cutoff` before run
- Decompress archive → assert bytes equal `origData[0:cutoff]` exactly
- Read source bytes at `[cutoff:origSize]` → assert equal `origData[cutoff:]`
- Writer's returned bytes present at end of source file

**Why byte-exact is achievable:** the writer only appends past EOF; logcut only reads/punches up to cutoff. Byte ranges never overlap.

## Test C — `TestIntegrationConcurrentWriterBurst`

**Scale:** 500 MB initial log, 3 concurrent writers (256 KB/s, unbounded, unbounded)  
**Logcut:** keep 10%, gzip, `--rate-limit 50M` (keeps logcut from finishing before writers generate data)

**Assertions:**
- `Run()` returns 0
- Output gzip valid
- Source sparse
- Each writer's unique sentinel line present in source

## Test D — `TestIntegrationDisasterRecovery`

**Scale:** dynamic — `logSize = statfs(tmpDir).Avail * 0.85` (targets ~90% disk usage)  
**Skip conditions:** `testing.Short()`, or available disk < 5 GB

**Generation:** write log in 64 MB chunks with realistic timestamped lines; loop until target size reached.

**Writers:** 3 concurrent writers (trickle / moderate / burst)

**Logcut:** keep 10%, `-g`, `--verify full`, `--chunk-timeout 10m`

**Assertions:**
- `Run()` returns 0
- Gzip decompresses without error
- Decompressed byte count == cutoff (original archived length)
- Source `BlocksUsed < Size`
- Disk free bytes after > disk free bytes before × 1.5 (meaningful recovery)
- All three writers' sentinel lines present in source tail

## Generation Strategy

For speed on large files, use a pre-built 64 KB line template repeated in 64 MB writes. At typical SSD speeds (500 MB/s), a 200 GB log generates in ~6 minutes — acceptable for a manual test.

## Error Handling

- `startConcurrentWriter` stops cleanly on `t.Cleanup` even if logcut panics
- Test D checks `err` from every `os.Write` in the generator and fails fast on disk-full during generation (which means the fill target was too aggressive — add 200 MB margin)

## Self-Review

- No TBDs or placeholders
- All four tests have distinct, non-overlapping assertions
- Test D is correctly gated behind `testing.Short()` and a size floor
- Byte-exact check in B is justified by the non-overlapping range invariant
- No contradictions between tests — each isolates one concern
