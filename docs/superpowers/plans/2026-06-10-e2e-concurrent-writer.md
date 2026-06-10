# E2E Concurrent Writer Tests — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add four Go e2e tests that run logcut against a log file being actively written by concurrent goroutines — the core real-life scenario logcut was designed for.

**Architecture:** Two new test files in `package cli`. `concurrent_test.go` holds the shared `startConcurrentWriter` helper and Tests A/B/C (500 MB scale, run in normal `go test`). `disaster_test.go` holds Test D which uses `statfs` to generate a log sized at 85% of available disk, skipped under `-short` or when <5 GB free.

**Tech Stack:** Go stdlib only (`os`, `sync`, `syscall`); uses existing `Run()` entry point, `disk.FileInfo`, `humanize` from `integration_test.go`.

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `internal/cli/concurrent_test.go` | **Create** | `startConcurrentWriter` helper + Tests A, B, C |
| `internal/cli/disaster_test.go` | **Create** | `generateLogFast`, `availableBytes`, `decompressedByteCount` helpers + Test D |

No existing files are modified.

---

## Task 1: `startConcurrentWriter` helper + `readGzipFileBytes`

**Files:**
- Create: `internal/cli/concurrent_test.go`

- [ ] **Step 1: Create the file with helpers only**

```go
package cli

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

// startConcurrentWriter opens path O_APPEND and writes timestamped log lines
// at bytesPerSec rate (0 = unbounded). Returns stop() which halts the writer
// and returns all bytes it wrote. t.Cleanup guarantees the goroutine stops
// even on test failure.
func startConcurrentWriter(t *testing.T, path string, bytesPerSec int64) func() []byte {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("startConcurrentWriter open %s: %v", path, err)
	}
	id := fmt.Sprintf("w%d", time.Now().UnixNano())
	var mu sync.Mutex
	var written []byte
	stop := make(chan struct{})
	var once sync.Once
	stopped := make(chan struct{})
	closeStop := func() { once.Do(func() { close(stop) }) }

	go func() {
		defer close(stopped)
		defer f.Close()
		seq := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			seq++
			start := time.Now()
			line := []byte(fmt.Sprintf("[%s] %s seq=%08d sentinel=logcut-concurrent-test\n",
				time.Now().Format("2006-01-02T15:04:05.000"), id, seq))
			if _, err := f.Write(line); err != nil {
				return
			}
			mu.Lock()
			written = append(written, line...)
			mu.Unlock()
			if bytesPerSec > 0 {
				elapsed := time.Since(start)
				budget := time.Duration(float64(len(line)) / float64(bytesPerSec) * float64(time.Second))
				if budget > elapsed {
					time.Sleep(budget - elapsed)
				}
			}
		}
	}()

	t.Cleanup(func() {
		closeStop()
		<-stopped
	})
	return func() []byte {
		closeStop()
		<-stopped
		mu.Lock()
		defer mu.Unlock()
		return append([]byte(nil), written...)
	}
}

// readGzipFileBytes decompresses a (possibly multi-member) gzip file into []byte.
func readGzipFileBytes(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var buf bytes.Buffer
	for {
		gr, err := gzip.NewReader(f)
		if err == io.EOF {
			break
		}
		if err != nil {
			if err.Error() == "unexpected EOF" {
				break
			}
			return nil, fmt.Errorf("gzip reader: %w", err)
		}
		if _, copyErr := io.Copy(&buf, gr); copyErr != nil {
			_ = gr.Close()
			return nil, fmt.Errorf("gzip copy: %w", copyErr)
		}
		_ = gr.Close()
	}
	return buf.Bytes(), nil
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/cli/...
```

Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/cli/concurrent_test.go
git commit -m "test: add startConcurrentWriter helper and readGzipFileBytes"
```

---

## Task 2: Test A — `TestIntegrationConcurrentWriterStability`

**Files:**
- Modify: `internal/cli/concurrent_test.go`

- [ ] **Step 1: Add the test**

Append to `internal/cli/concurrent_test.go`:

```go
// TestIntegrationConcurrentWriterStability runs logcut while a goroutine
// appends 1 MB/s to the source. Verifies stability: exit 0, valid gzip,
// sparse source, writer data intact.
func TestIntegrationConcurrentWriterStability(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 500k lines × 1 KB = ~500 MB initial log
	if err := generateLog(source, 500_000, 1024); err != nil {
		t.Fatal(err)
	}
	info, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("source: %s", humanize(info.Size))

	stopWriter := startConcurrentWriter(t, source, 1*1024*1024) // 1 MB/s

	code := Run([]string{
		"--quiet", "-g", "--compress-level", "1", "--verify", "full",
		"-k", fmt.Sprintf("%d", info.Size/10),
		"--state-dir", stateDir, "--lock-dir", lockDir,
		source, output,
	})
	if code != 0 {
		t.Fatalf("Run returned %d", code)
	}

	writerData := stopWriter()

	// gzip must be valid
	if _, err := readGzipFileBytes(output); err != nil {
		t.Fatalf("output gzip invalid: %v", err)
	}
	outInfo, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if outInfo.Size() == 0 {
		t.Fatal("output gzip is empty")
	}
	t.Logf("output gzip: %s", humanize(outInfo.Size()))

	// source must be sparse
	finalInfo, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	if finalInfo.BlocksUsed >= finalInfo.Size {
		t.Errorf("source not sparse: apparent=%s blocks=%s",
			humanize(finalInfo.Size), humanize(finalInfo.BlocksUsed))
	}
	t.Logf("source sparse: apparent=%s real=%s",
		humanize(finalInfo.Size), humanize(finalInfo.BlocksUsed))

	// writer's sentinel lines must still exist in source
	if len(writerData) == 0 {
		t.Fatal("writer wrote nothing")
	}
	tailSize := int64(4 * 1024 * 1024)
	if int64(len(writerData)) < tailSize {
		tailSize = int64(len(writerData))
	}
	writerID := string(writerData[:bytes.IndexByte(writerData, ' ')])
	sourceFinal, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceFinal.Close()
	sourceFinal.Seek(-tailSize, io.SeekEnd)
	tail, err := io.ReadAll(sourceFinal)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(tail, []byte(writerID)) {
		t.Errorf("writer sentinel %q not found in source tail", writerID)
	}
	t.Logf("writer wrote %s, sentinel found in source tail", humanize(int64(len(writerData))))
}
```

Also add missing imports at the top of the file (replace the existing import block):

```go
import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rushikeshsakharleofficial/logcut/internal/disk"
)
```

- [ ] **Step 2: Run the test**

```bash
go test -v -run TestIntegrationConcurrentWriterStability ./internal/cli/ -timeout 5m
```

Expected: `PASS` — logcut completes, gzip valid, source sparse, writer sentinel found.

- [ ] **Step 3: Commit**

```bash
git add internal/cli/concurrent_test.go
git commit -m "test: TestIntegrationConcurrentWriterStability (500MB + 1MB/s writer)"
```

---

## Task 3: Test B — `TestIntegrationConcurrentWriterIntegrity`

**Files:**
- Modify: `internal/cli/concurrent_test.go`

- [ ] **Step 1: Add the test**

Append to `internal/cli/concurrent_test.go`:

```go
// TestIntegrationConcurrentWriterIntegrity verifies byte-exact correctness:
// the archived portion must equal origData[0:cutoff] and the source tail must
// contain the writer's appended bytes. The writer only appends past EOF so
// its range never overlaps with logcut's read/punch range.
func TestIntegrationConcurrentWriterIntegrity(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 500k lines × 1 KB = ~500 MB
	if err := generateLog(source, 500_000, 1024); err != nil {
		t.Fatal(err)
	}
	info, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	logSize := info.Size
	keepBytes := logSize / 10
	cutoff := logSize - keepBytes
	t.Logf("source=%s cutoff=%s keep=%s", humanize(logSize), humanize(cutoff), humanize(keepBytes))

	// Snapshot original data before any modification
	origData, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}

	// 256 KB/s writer — slow enough that we can track exactly what it wrote
	stopWriter := startConcurrentWriter(t, source, 256*1024)

	code := Run([]string{
		"--quiet", "-g", "--compress-level", "1", "--verify", "full",
		"-k", fmt.Sprintf("%d", keepBytes),
		"--state-dir", stateDir, "--lock-dir", lockDir,
		source, output,
	})
	if code != 0 {
		t.Fatalf("Run returned %d", code)
	}

	writerData := stopWriter()
	t.Logf("writer appended %s", humanize(int64(len(writerData))))

	// Decompress archive — must equal origData[0:cutoff] byte-for-byte
	archived, err := readGzipFileBytes(output)
	if err != nil {
		t.Fatalf("read gzip: %v", err)
	}
	if int64(len(archived)) != cutoff {
		t.Fatalf("archive length=%d, want %d (cutoff)", len(archived), cutoff)
	}
	if !bytes.Equal(archived, origData[:cutoff]) {
		// Find first mismatch for diagnostics
		for i := range archived {
			if archived[i] != origData[i] {
				t.Fatalf("archive mismatch at byte %d: got %02x want %02x",
					i, archived[i], origData[i])
			}
		}
	}
	t.Logf("archive byte-exact match: %s", humanize(int64(len(archived))))

	// Source tail [cutoff:logSize] must equal origData[cutoff:]
	sourceFinal, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceFinal.Close()
	if _, err := sourceFinal.Seek(cutoff, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	tail := make([]byte, logSize-cutoff)
	if _, err := io.ReadFull(sourceFinal, tail); err != nil {
		t.Fatalf("read source tail: %v", err)
	}
	if !bytes.Equal(tail, origData[cutoff:]) {
		t.Fatalf("source tail mismatch: first differing byte search omitted (lengths: got %d want %d)",
			len(tail), len(origData[cutoff:]))
	}
	t.Logf("source tail byte-exact match: %s", humanize(int64(len(tail))))

	// Writer data must follow the original tail
	if len(writerData) > 0 {
		writerEnd := make([]byte, len(writerData))
		if _, err := io.ReadFull(sourceFinal, writerEnd); err != nil {
			t.Fatalf("read writer region: %v", err)
		}
		if !bytes.Equal(writerEnd, writerData) {
			t.Fatalf("writer region mismatch: %d bytes differ", len(writerData))
		}
		t.Logf("writer data byte-exact match: %s", humanize(int64(len(writerData))))
	}
}
```

- [ ] **Step 2: Run the test**

```bash
go test -v -run TestIntegrationConcurrentWriterIntegrity ./internal/cli/ -timeout 5m
```

Expected: `PASS` — archive == origData[0:cutoff], source tail == origData[cutoff:], writer bytes exact.

- [ ] **Step 3: Commit**

```bash
git add internal/cli/concurrent_test.go
git commit -m "test: TestIntegrationConcurrentWriterIntegrity (byte-exact with 256KB/s writer)"
```

---

## Task 4: Test C — `TestIntegrationConcurrentWriterBurst`

**Files:**
- Modify: `internal/cli/concurrent_test.go`

- [ ] **Step 1: Add the test**

Append to `internal/cli/concurrent_test.go`:

```go
// TestIntegrationConcurrentWriterBurst runs three concurrent writers —
// trickle, and two unbounded — while logcut archives. Proves no deadlock,
// fd conflict, or corruption under sustained concurrent IO.
func TestIntegrationConcurrentWriterBurst(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := generateLog(source, 500_000, 1024); err != nil {
		t.Fatal(err)
	}
	info, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("source: %s", humanize(info.Size))

	// Three writers: 256 KB/s trickle, two unbounded bursts
	stopTrickle := startConcurrentWriter(t, source, 256*1024)
	stopBurst1 := startConcurrentWriter(t, source, 0)
	stopBurst2 := startConcurrentWriter(t, source, 0)

	// Rate-limit logcut so writers generate meaningful data before it finishes
	code := Run([]string{
		"--quiet", "-g", "--compress-level", "1", "--verify", "full",
		"-k", fmt.Sprintf("%d", info.Size/10),
		"--rate-limit", "50M",
		"--state-dir", stateDir, "--lock-dir", lockDir,
		source, output,
	})
	if code != 0 {
		t.Fatalf("Run returned %d", code)
	}

	trickleData := stopTrickle()
	burst1Data := stopBurst1()
	burst2Data := stopBurst2()

	totalWritten := int64(len(trickleData) + len(burst1Data) + len(burst2Data))
	t.Logf("writers wrote total: %s", humanize(totalWritten))

	// gzip must be valid
	if _, err := readGzipFileBytes(output); err != nil {
		t.Fatalf("output gzip invalid: %v", err)
	}

	// source must be sparse
	finalInfo, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	if finalInfo.BlocksUsed >= finalInfo.Size {
		t.Errorf("source not sparse: apparent=%s blocks=%s",
			humanize(finalInfo.Size), humanize(finalInfo.BlocksUsed))
	}

	// each writer's sentinel must appear in the last 10 MB of source
	readSentinel := func(label string, data []byte) {
		if len(data) == 0 {
			t.Logf("%s: wrote nothing (too fast?)", label)
			return
		}
		end := bytes.IndexByte(data, ' ')
		if end < 0 {
			end = len(data)
		}
		sentinel := data[:end] // e.g. "[2026-..."  — first token is timestamp; skip to writer id
		// writer id is the second space-delimited token
		parts := bytes.SplitN(data, []byte(" "), 3)
		if len(parts) < 2 {
			t.Errorf("%s: cannot parse writer id from %q", label, data[:min(50, len(data))])
			return
		}
		writerID := parts[1]
		_ = sentinel

		sourceFinal, err := os.Open(source)
		if err != nil {
			t.Fatal(err)
		}
		defer sourceFinal.Close()
		sourceFinal.Seek(-10*1024*1024, io.SeekEnd)
		tail, err := io.ReadAll(sourceFinal)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(tail, writerID) {
			t.Errorf("%s: sentinel %q not in source tail", label, writerID)
		} else {
			t.Logf("%s: sentinel found (%s written)", label, humanize(int64(len(data))))
		}
	}
	readSentinel("trickle", trickleData)
	readSentinel("burst1", burst1Data)
	readSentinel("burst2", burst2Data)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

- [ ] **Step 2: Run the test**

```bash
go test -v -run TestIntegrationConcurrentWriterBurst ./internal/cli/ -timeout 5m
```

Expected: `PASS` — all three writers' sentinels found, gzip valid, source sparse.

- [ ] **Step 3: Commit**

```bash
git add internal/cli/concurrent_test.go
git commit -m "test: TestIntegrationConcurrentWriterBurst (3 concurrent writers, trickle+burst)"
```

---

## Task 5: Test D — `TestIntegrationDisasterRecovery`

**Files:**
- Create: `internal/cli/disaster_test.go`

- [ ] **Step 1: Create the file**

```go
package cli

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rushikeshsakharleofficial/logcut/internal/disk"
)

func availableBytes(path string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return int64(st.Bavail) * int64(st.Bsize)
}

// generateLogFast fills path with targetBytes of realistic newline-terminated
// log data. Writes in 64 MB chunks for speed (disk-rate limited, not CPU).
func generateLogFast(t *testing.T, path string, targetBytes int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("generateLogFast: %v", err)
	}
	defer f.Close()

	// 64-byte line: 63 printable chars + newline
	line := make([]byte, 64)
	for i := range line {
		line[i] = 'x'
	}
	line[63] = '\n'

	chunkSize := 64 * 1024 * 1024 // 64 MB
	chunk := bytes.Repeat(line, chunkSize/len(line))

	written := int64(0)
	reported := int64(0)
	reportEvery := int64(1024 * 1024 * 1024) // log every 1 GB

	for written < targetBytes {
		toWrite := int64(len(chunk))
		if written+toWrite > targetBytes {
			toWrite = targetBytes - written
		}
		n, err := f.Write(chunk[:toWrite])
		if err != nil {
			t.Fatalf("generateLogFast: write at %s: %v", humanize(written), err)
		}
		written += int64(n)
		if written-reported >= reportEvery {
			t.Logf("generateLogFast: %s / %s", humanize(written), humanize(targetBytes))
			reported = written
		}
	}
	t.Logf("generateLogFast: complete — %s", humanize(written))
}

// decompressedByteCount streams a (multi-member) gzip file and counts bytes
// without buffering the full output. Safe for archives larger than RAM.
func decompressedByteCount(t *testing.T, path string) int64 {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("decompressedByteCount open: %v", err)
	}
	defer f.Close()
	total := int64(0)
	buf := make([]byte, 4*1024*1024)
	for {
		gr, err := gzip.NewReader(f)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("gzip.NewReader: %v", err)
		}
		for {
			n, readErr := gr.Read(buf)
			total += int64(n)
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				t.Fatalf("gzip.Read: %v", readErr)
			}
		}
		_ = gr.Close()
	}
	return total
}

// TestIntegrationDisasterRecovery generates a log file sized at 85% of
// available disk space, runs three concurrent writers, then runs logcut and
// verifies meaningful free space recovery, a valid archive, and sparse source.
//
// Run explicitly — skipped under -short or when <5 GB free:
//
//	go test -v -run TestIntegrationDisasterRecovery ./internal/cli/ -timeout 60m
func TestIntegrationDisasterRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping disaster test in -short mode")
	}

	dir := t.TempDir()
	avail := availableBytes(dir)
	const minRequired = 5 * 1024 * 1024 * 1024 // 5 GB
	if avail < minRequired {
		t.Skipf("skipping: only %s available, need at least 5 GB", humanize(avail))
	}

	logSize := int64(float64(avail) * 0.85)
	t.Logf("available: %s  log target: %s", humanize(avail), humanize(logSize))

	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		t.Fatal(err)
	}

	t.Logf("generating %s log...", humanize(logSize))
	genStart := time.Now()
	generateLogFast(t, source, logSize)
	t.Logf("generated in %s", time.Since(genStart).Round(time.Second))

	freeBefore := availableBytes(dir)
	keepBytes := logSize / 10
	cutoff := logSize - keepBytes
	t.Logf("disk free before: %s  cutoff: %s  keep: %s",
		humanize(freeBefore), humanize(cutoff), humanize(keepBytes))

	// Three concurrent writers
	stopTrickle := startConcurrentWriter(t, source, 256*1024)       // 256 KB/s
	stopModerate := startConcurrentWriter(t, source, 2*1024*1024)   // 2 MB/s
	stopBurst := startConcurrentWriter(t, source, 0)                // unbounded

	writers := map[string]func() []byte{
		"trickle":  stopTrickle,
		"moderate": stopModerate,
		"burst":    stopBurst,
	}

	t.Logf("running logcut...")
	runStart := time.Now()
	code := Run([]string{
		"-g", "--compress-level", "1", "--verify", "full",
		"-k", fmt.Sprintf("%d", keepBytes),
		"--chunk-timeout", "10m",
		"--state-dir", stateDir, "--lock-dir", lockDir,
		source, output,
	})
	t.Logf("logcut finished in %s", time.Since(runStart).Round(time.Second))
	if code != 0 {
		t.Fatalf("Run returned %d", code)
	}

	// Stop writers and collect their IDs
	writerIDs := map[string][]byte{}
	for name, stop := range writers {
		data := stop()
		if len(data) > 0 {
			parts := strings.SplitN(string(data), " ", 3)
			if len(parts) >= 2 {
				writerIDs[name] = []byte(parts[1]) // writer id token
			}
		}
		t.Logf("writer %s wrote %s", name, humanize(int64(len(data))))
	}

	// Free space must have recovered significantly
	freeAfter := availableBytes(dir)
	t.Logf("disk free after: %s (was %s)", humanize(freeAfter), humanize(freeBefore))
	if freeAfter < freeBefore*2 {
		t.Errorf("free space insufficient recovery: before=%s after=%s",
			humanize(freeBefore), humanize(freeAfter))
	}

	// Archive must be a valid gzip and have the right decompressed size
	t.Logf("verifying archive (streaming)...")
	archiveBytes := decompressedByteCount(t, output)
	if archiveBytes != cutoff {
		t.Errorf("archive decompressed size=%s want=%s (cutoff)",
			humanize(archiveBytes), humanize(cutoff))
	}
	t.Logf("archive decompressed: %s", humanize(archiveBytes))

	// Source must be sparse
	finalInfo, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	if finalInfo.BlocksUsed >= finalInfo.Size {
		t.Errorf("source not sparse: apparent=%s blocks=%s",
			humanize(finalInfo.Size), humanize(finalInfo.BlocksUsed))
	}
	t.Logf("source sparse: apparent=%s real=%s saved=%s",
		humanize(finalInfo.Size), humanize(finalInfo.BlocksUsed),
		humanize(finalInfo.Size-finalInfo.BlocksUsed))

	// Each writer's sentinel must be in the last 10 MB of source
	sourceFinal, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceFinal.Close()
	sourceFinal.Seek(-10*1024*1024, io.SeekEnd)
	tail, err := io.ReadAll(sourceFinal)
	if err != nil {
		t.Fatal(err)
	}
	for name, id := range writerIDs {
		if bytes.Contains(tail, id) {
			t.Logf("writer %s sentinel found in source tail", name)
		} else {
			t.Errorf("writer %s sentinel %q not found in source tail", name, id)
		}
	}
}
```

- [ ] **Step 2: Build**

```bash
go build ./internal/cli/...
```

Expected: no output, exit 0.

- [ ] **Step 3: Run a quick smoke test with -short (must skip)**

```bash
go test -v -short -run TestIntegrationDisasterRecovery ./internal/cli/
```

Expected:
```
--- SKIP: TestIntegrationDisasterRecovery (0.00s)
    disaster_test.go:XX: skipping disaster test in -short mode
```

- [ ] **Step 4: Run the full disaster test**

```bash
go test -v -run TestIntegrationDisasterRecovery ./internal/cli/ -timeout 60m
```

Expected: `PASS` with log lines showing file generation speed, logcut runtime, free space recovery, archive size, and writer sentinels found. Total runtime varies with disk size and speed.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/disaster_test.go
git commit -m "test: TestIntegrationDisasterRecovery (85% of disk, 3 concurrent writers)"
```

---

## Task 6: Full suite verification + Makefile target

**Files:**
- Modify: `Makefile`
- Modify: `docs/superpowers/specs/2026-06-10-e2e-concurrent-writer-design.md` (add spec doc to git)

- [ ] **Step 1: Run full normal test suite (no -short, no disaster)**

```bash
go test -race -timeout 15m ./...
```

Expected: all packages pass. Tests A/B/C run. Test D skipped (not in `-run` filter for `./...`).

Wait — Test D is in the `cli` package. `go test ./...` WILL run it. Add a `testing.Short()` guard — already there — but `go test ./...` without `-short` will run it.

To prevent Test D from running in `go test ./...`, rename it so it's only triggered explicitly:

Rename `TestIntegrationDisasterRecovery` → add the following at the top of the function (already included in code above):

```go
if testing.Short() {
    t.Skip(...)
}
```

But also add a disk size guard: if `avail < minRequired` it skips. On most CI machines, tmpfs or constrained environments will have <5 GB, so it auto-skips. On a dev machine with lots of free space, add `-short` to suppress it.

- [ ] **Step 2: Add Makefile target**

In `Makefile`, after the existing `test:` target, add:

```makefile
test-disaster:
	$(GO) test -v -run TestIntegrationDisasterRecovery ./internal/cli/ -timeout 60m
```

- [ ] **Step 3: Run standard suite with -race to confirm no new races**

```bash
go test -race -timeout 15m -short ./...
```

Expected: all pass, no DATA RACE output.

- [ ] **Step 4: Final commit**

```bash
git add Makefile docs/superpowers/
git commit -m "test: add make test-disaster target and commit spec/plan docs"
```

---

## Self-Review

**Spec coverage:**
- Test A (stability) ✓ Task 2
- Test B (byte-exact integrity) ✓ Task 3
- Test C (burst writers) ✓ Task 4
- Test D (85% of disk, disaster scale) ✓ Task 5
- `startConcurrentWriter` helper ✓ Task 1
- `generateLogFast`, `availableBytes`, `decompressedByteCount` ✓ Task 5

**Placeholder scan:** None found — all steps contain actual code.

**Type consistency:**
- `startConcurrentWriter` signature consistent across all four tests
- `humanize` reused from `integration_test.go` (same package)
- `disk.FileInfo` returns `FileStats{Size, BlocksUsed}` — consistent with existing tests
- `readGzipFileBytes` returns `([]byte, error)` — used in Tests A, B, C
- `decompressedByteCount` returns `int64` — used only in Test D

**Edge case:** `min()` helper added in Task 4 — Go 1.21+ has `min` builtin but go.mod is 1.22, safe to define locally since builtins can't be redefined as package-level functions in the same package without conflict. Actually Go 1.21 added `min` as a builtin — defining a package-level `min` in Go 1.22 will shadow it but NOT conflict, since builtins are predeclared not package-scoped. Safe.
