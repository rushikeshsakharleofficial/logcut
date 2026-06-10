package cli

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// generateLogFast fills path with targetBytes of newline-terminated log data.
// Writes in 64 MB chunks for speed (disk-rate limited, not CPU-rate limited).
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
// without buffering. Safe for archives larger than available RAM.
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
// verifies meaningful free space recovery, a valid sparse archive, and that
// all writer sentinels survive in the source tail.
//
// Run explicitly — skipped under -short or when < 5 GB free:
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
		t.Skipf("only %s available, need ≥5 GB", humanize(avail))
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

	stopTrickle := startConcurrentWriter(t, source, 256*1024)     // 256 KB/s
	stopModerate := startConcurrentWriter(t, source, 2*1024*1024) // 2 MB/s
	stopBurst := startConcurrentWriter(t, source, 0)              // unbounded

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

	trickleData := stopTrickle()
	moderateData := stopModerate()
	burstData := stopBurst()
	t.Logf("writers: trickle=%s moderate=%s burst=%s",
		humanize(int64(len(trickleData))),
		humanize(int64(len(moderateData))),
		humanize(int64(len(burstData))))

	// Disk free must have recovered significantly
	freeAfter := availableBytes(dir)
	t.Logf("disk free after: %s (was %s before log)", humanize(freeAfter), humanize(freeBefore))
	if freeAfter < freeBefore*2 {
		t.Errorf("insufficient free space recovery: before=%s after=%s",
			humanize(freeBefore), humanize(freeAfter))
	}

	// Archive must decompress to exactly cutoff bytes
	t.Logf("verifying archive (streaming)...")
	archiveBytes := decompressedByteCount(t, output)
	if archiveBytes != cutoff {
		t.Errorf("archive decompressed=%s want=%s (cutoff)", humanize(archiveBytes), humanize(cutoff))
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

	// Each writer's sentinel must appear in last 10 MB of source
	sf, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()
	if _, err := sf.Seek(-10*1024*1024, io.SeekEnd); err != nil {
		sf.Seek(0, io.SeekStart)
	}
	tail, err := io.ReadAll(sf)
	if err != nil {
		t.Fatal(err)
	}
	for label, data := range map[string][]byte{
		"trickle":  trickleData,
		"moderate": moderateData,
		"burst":    burstData,
	} {
		if len(data) == 0 {
			t.Logf("writer %s: wrote nothing", label)
			continue
		}
		id := writerID(data)
		if id == nil {
			t.Errorf("writer %s: cannot parse id", label)
			continue
		}
		if bytes.Contains(tail, id) {
			t.Logf("writer %s: sentinel found", label)
		} else {
			t.Errorf("writer %s: sentinel %q not found in source tail", label, id)
		}
	}
}
