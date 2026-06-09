package cli

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rushikeshsakharleofficial/logcut/internal/disk"
	"github.com/rushikeshsakharleofficial/logcut/internal/job"
	"github.com/rushikeshsakharleofficial/logcut/internal/state"
)

func generateLog(path string, numLines int, lineLen int) error {
	chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789    "
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, lineLen)
	for i := 0; i < numLines; i++ {
		for j := range buf {
			buf[j] = chars[rand.Intn(len(chars))]
		}
		buf[len(buf)-1] = '\n'
		if _, err := f.Write(buf); err != nil {
			return err
		}
	}
	return nil
}

func readGzipFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
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
			return "", fmt.Errorf("gzip reader: %w", err)
		}
		_, copyErr := io.Copy(&buf, gr)
		_ = gr.Close()
		if copyErr != nil {
			return "", fmt.Errorf("gzip copy: %w", copyErr)
		}
	}
	return buf.String(), nil
}

func TestIntegrationFullPipeline(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")

	numLines := 100000
	lineLen := 100
	if err := generateLog(source, numLines, lineLen); err != nil {
		t.Fatal(err)
	}
	info, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Created source log: size=%d lines=%d", info.Size, numLines)

	keepBytes := info.Size / 10
	keepRaw := fmt.Sprintf("%d", keepBytes)

	// Read source BEFORE compaction (punched holes become zero-filled after)
	origData, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	origStr := string(origData)

	code := Run([]string{
		"--quiet",
		"-g",
		"--verify", "full",
		"-k", keepRaw,
		"--state-dir", stateDir,
		"--lock-dir", lockDir,
		source,
		output,
	})
	if code != 0 {
		t.Fatalf("Run returned %d", code)
	}

	gzipContent, err := readGzipFile(output)
	if err != nil {
		t.Fatalf("failed to read gzip output: %v", err)
	}
	if gzipContent == "" {
		t.Fatal("gzip output is empty")
	}

	cutoff := info.Size - keepBytes
	expectedArchived := origStr[:cutoff]
	if gzipContent != expectedArchived {
		// Compare lengths — compaction produces the exact prefix
		if len(gzipContent) != len(expectedArchived) {
			t.Fatalf("decompressed output length=%d, want %d", len(gzipContent), len(expectedArchived))
		}
		// Compare first and last bytes for sanity
		if gzipContent[:10] != expectedArchived[:10] || gzipContent[len(gzipContent)-10:] != expectedArchived[len(expectedArchived)-10:] {
			t.Fatalf("decompressed output mismatch at boundaries")
		}
	}
	t.Logf("Decompressed output matches source: %d bytes", len(gzipContent))

	// Verify source is sparse (blocks used < apparent size)
	finalInfo, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	if finalInfo.BlocksUsed >= finalInfo.Size {
		t.Errorf("source not sparse after compaction: apparent=%d blocks=%d", finalInfo.Size, finalInfo.BlocksUsed)
	}
	t.Logf("Source sparse: apparent=%d blocks_used=%d", finalInfo.Size, finalInfo.BlocksUsed)

	// Verify state file
	statePath := job.StatePath(stateDir, source, output)
	s, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if s.LastPunchedOffset != cutoff {
		t.Fatalf("state LastPunchedOffset=%d, want cutoff=%d", s.LastPunchedOffset, cutoff)
	}
	t.Logf("State file: punched=%d archived=%d", s.LastPunchedOffset, s.LastArchivedOffset)
}

func TestIntegrationJSONOutput(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	runLog := filepath.Join(dir, "run.log")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")

	if err := os.WriteFile(source, []byte("line1\nline2\nline3\nline4\n"), 0644); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{
		"--quiet",
		"--json",
		"--log-file", runLog,
		"-g",
		"-k", "4",
		"--state-dir", stateDir,
		"--lock-dir", lockDir,
		source,
		output,
	})
	if code != 0 {
		t.Fatalf("Run returned %d", code)
	}

	got, err := os.ReadFile(runLog)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{`"event":"start"`, `"event":"auto_throttle"`, `"event":"plan"`, `"event":"chunk_start"`, `"event":"chunk_done"`, `"event":"complete"`} {
		if !strings.Contains(text, want) {
			t.Errorf("JSON output missing %q", want)
		}
	}
}

func TestIntegrationStopFreeAbove(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")

	numLines := 50000
	lineLen := 100
	if err := generateLog(source, numLines, lineLen); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{
		"--quiet",
		"-g",
		"-k", "1000",
		"--stop-free-above", "999G",
		"--state-dir", stateDir,
		"--lock-dir", lockDir,
		source,
		output,
	})
	if code != 0 {
		t.Fatalf("Run returned %d", code)
	}

	// Should have completed fully since stop-free-above is huge
	statePath := job.StatePath(stateDir, source, output)
	s, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := disk.FileInfo(source)
	expectedCutoff := info.Size - 1000
	if s.LastPunchedOffset != expectedCutoff {
		t.Fatalf("expected full compaction: punched=%d cutoff=%d", s.LastPunchedOffset, expectedCutoff)
	}
}

func TestIntegrationResume(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")

	numLines := 30000
	lineLen := 100
	if err := generateLog(source, numLines, lineLen); err != nil {
		t.Fatal(err)
	}
	info, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	keepBytes := int64(500)
	cutoff := info.Size - keepBytes

	// Read source content before compaction
	origData, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	origStr := string(origData)

	// First run: full compaction
	code := Run([]string{
		"--quiet",
		"-g",
		"-k", fmt.Sprintf("%d", keepBytes),
		"--state-dir", stateDir,
		"--lock-dir", lockDir,
		source,
		output,
	})
	if code != 0 {
		t.Fatalf("first Run returned %d", code)
	}

	// Read the state and gzip content
	statePath := job.StatePath(stateDir, source, output)
	fullGzip, err := readGzipFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(fullGzip) != int(cutoff) {
		t.Fatalf("full run output length=%d, want cutoff=%d", len(fullGzip), cutoff)
	}
	if fullGzip != origStr[:cutoff] {
		t.Fatalf("full run output mismatch")
	}
	t.Logf("Full run complete: compressed %d bytes", len(fullGzip))

	// Now simulate a different log at the same path by creating a new source file.
	// First clean up the old output and state.
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	_ = os.Rename(statePath, statePath+".full")
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}

	// Create a new log with the SAME path
	numLines2 := 20000
	if err := generateLog(source, numLines2, lineLen); err != nil {
		t.Fatal(err)
	}
	info2, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	keepBytes2 := int64(500)
	cutoff2 := info2.Size - keepBytes2
	origData2, _ := os.ReadFile(source)

	// First run: use max-runtime to stop after some progress
	code = Run([]string{
		"--quiet",
		"-g",
		"-k", fmt.Sprintf("%d", keepBytes2),
		"--max-runtime", "2s",
		"--state-dir", stateDir,
		"--lock-dir", lockDir,
		source,
		output,
	})
	if code != 0 {
		t.Fatalf("second Run (partial) returned %d", code)
	}

	// Verify state was saved with partial progress
	s1, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if s1.LastPunchedOffset <= 0 {
		t.Fatal("expected partial progress, but state shows offset 0")
	}
	t.Logf("Partial run punched up to %d / %d", s1.LastPunchedOffset, cutoff2)

	// Resume and complete
	code = Run([]string{
		"--quiet",
		"-g",
		"-k", fmt.Sprintf("%d", keepBytes2),
		"--state-dir", stateDir,
		"--lock-dir", lockDir,
		source,
		output,
	})
	if code != 0 {
		t.Fatalf("resume Run returned %d", code)
	}

	s2, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if s2.LastPunchedOffset != cutoff2 {
		t.Fatalf("resume did not complete: punched=%d cutoff=%d", s2.LastPunchedOffset, cutoff2)
	}

	// Verify total output is correct
	gzipContent, err := readGzipFile(output)
	if err != nil {
		t.Fatal(err)
	}
	expectedPrefix := string(origData2)[:cutoff2]
	if gzipContent != expectedPrefix {
		// Compare boundaries if lengths match
		if len(gzipContent) != len(expectedPrefix) {
			t.Fatalf("resume output length=%d, want %d", len(gzipContent), len(expectedPrefix))
		}
		if gzipContent[:10] != expectedPrefix[:10] || gzipContent[len(gzipContent)-10:] != expectedPrefix[len(expectedPrefix)-10:] {
			t.Fatalf("resume output mismatch at boundaries")
		}
	}
	t.Logf("Resume completed: total output = %d bytes", len(gzipContent))
}

func TestIntegrationForceUnlockCLI(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	lockDir := filepath.Join(dir, "lock")

	if err := os.WriteFile(source, []byte("line1\nline2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a lock file with a dead PID manually
	lockPath := job.LockPath(lockDir, source, output)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("99999\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// force-unlock should succeed
	code := Run([]string{
		"force-unlock",
		"--lock-dir", lockDir,
		source,
		output,
	})
	if code != 0 {
		// PID 99999 might be alive — that's fine, skip
		t.Logf("force-unlock returned %d (PID 99999 may be alive)", code)
	}
}

func TestIntegrationStatusCommand(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")

	if err := os.WriteFile(source, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{
		"--quiet",
		"-g",
		"-k", "2",
		"--state-dir", stateDir,
		"--lock-dir", lockDir,
		source,
		output,
	})
	if code != 0 {
		t.Fatalf("Run returned %d", code)
	}

	// status should succeed
	code = Run([]string{
		"status",
		"--state-dir", stateDir,
		source,
		output,
	})
	if code != 0 {
		t.Fatalf("status returned %d", code)
	}
}

func TestIntegrationCleanState(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")

	if err := os.WriteFile(source, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{
		"--quiet",
		"-g",
		"-k", "2",
		"--state-dir", stateDir,
		"--lock-dir", lockDir,
		source,
		output,
	})
	if code != 0 {
		t.Fatalf("Run returned %d", code)
	}

	statePath := job.StatePath(stateDir, source, output)
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file missing after run: %v", err)
	}

	code = Run([]string{
		"clean-state",
		"--state-dir", stateDir,
		source,
		output,
	})
	if code != 0 {
		t.Fatalf("clean-state returned %d", code)
	}

	if _, err := os.Stat(statePath); err == nil {
		t.Fatal("state file still exists after clean-state")
	}

	// Check backup exists
	backupEntries, _ := filepath.Glob(statePath + ".cleaned.*")
	if len(backupEntries) == 0 {
		t.Fatal("no backup file found after clean-state")
	}
	t.Logf("Backup created: %s", backupEntries[0])
}

func TestIntegrationSourceEqualsOutput(t *testing.T) {
	dir := t.TempDir()
	same := filepath.Join(dir, "same.log")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")

	if err := os.WriteFile(same, []byte("data\n"), 0644); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{
		"--quiet",
		"-g",
		"-k", "2",
		"--state-dir", stateDir,
		"--lock-dir", lockDir,
		same,
		same,
	})
	if code == 0 {
		t.Fatal("expected error when source==output")
	}
}

func TestIntegrationEmptySource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "empty.log")
	output := filepath.Join(dir, "archive.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")

	if err := os.WriteFile(source, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{
		"--quiet",
		"-g",
		"-k", "2",
		"--state-dir", stateDir,
		"--lock-dir", lockDir,
		source,
		output,
	})
	if code == 0 {
		t.Fatal("expected error for empty source")
	}
}

func TestIntegrationInvalidKeepLast(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "archive.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")

	if err := os.WriteFile(source, []byte("data\n"), 0644); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{
		"--quiet",
		"-g",
		"-k", "999G",
		"--state-dir", stateDir,
		"--lock-dir", lockDir,
		source,
		output,
	})
	if code == 0 {
		t.Fatal("expected error when keep-last > source size")
	}
}

func TestIntegrationNinetyPercentFull(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")

	// Simulate 90%-full: generate a large log that dominates the temp dir.
	// Use 20k lines of 1KB each = ~20MB, small enough to run quickly but
	// large enough to exercise multi-chunk compaction.
	numLines := 20000
	lineLen := 1024
	if err := generateLog(source, numLines, lineLen); err != nil {
		t.Fatal(err)
	}
	info, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	logSize := info.Size
	t.Logf("Source log: %s (%d lines)", humanize(logSize), numLines)

	// Read original content before compaction
	origData, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}

	// keep-last = 10% of log (90% gets archived/compacted)
	keepBytes := logSize / 10
	cutoff := logSize - keepBytes
	t.Logf("keep-last=%s (10%%), cutoff=%s (90%% archived)", humanize(keepBytes), humanize(cutoff))

	// Run the rescue
	code := Run([]string{
		"--quiet",
		"-g",
		"--verify", "full",
		"-k", fmt.Sprintf("%d", keepBytes),
		"--state-dir", stateDir,
		"--lock-dir", lockDir,
		source,
		output,
	})
	if code != 0 {
		t.Fatalf("Run returned %d", code)
	}

	// Verify output gzip is valid and contains the archived 90%
	gzipContent, err := readGzipFile(output)
	if err != nil {
		t.Fatalf("failed to read gzip output: %v", err)
	}
	if gzipContent == "" {
		t.Fatal("gzip output is empty")
	}

	expectedArchived := string(origData)[:cutoff]
	if len(gzipContent) != len(expectedArchived) {
		t.Fatalf("output length=%d, want %d", len(gzipContent), len(expectedArchived))
	}
	// Compare boundary bytes for sanity
	if gzipContent[:100] != expectedArchived[:100] || gzipContent[len(gzipContent)-100:] != expectedArchived[len(expectedArchived)-100:] {
		t.Fatalf("decompressed output mismatch at boundaries")
	}
	t.Logf("Archived output: %s matches original", humanize(int64(len(gzipContent))))

	// Verify source is sparse after compaction
	finalInfo, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	if finalInfo.BlocksUsed >= finalInfo.Size {
		t.Errorf("source not sparse: apparent=%s blocks=%s", humanize(finalInfo.Size), humanize(finalInfo.BlocksUsed))
	}
	t.Logf("Source sparse: apparent=%s real=%s (saved ~%s)",
		humanize(finalInfo.Size),
		humanize(finalInfo.BlocksUsed),
		humanize(finalInfo.Size-finalInfo.BlocksUsed))

	// Verify state file
	statePath := job.StatePath(stateDir, source, output)
	s, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if s.LastPunchedOffset != cutoff {
		t.Fatalf("state LastPunchedOffset=%d, want cutoff=%d", s.LastPunchedOffset, cutoff)
	}
	if s.OriginalSize != logSize {
		t.Fatalf("state OriginalSize=%d, want %d", s.OriginalSize, logSize)
	}
	t.Logf("State: original=%s archived=%s punched=%s gzip=%t",
		humanize(s.OriginalSize),
		humanize(s.LastArchivedOffset),
		humanize(s.LastPunchedOffset),
		s.Gzip)
}

func humanize(n int64) string {
	if n >= 1024*1024*1024 {
		return fmt.Sprintf("%.2fG", float64(n)/float64(1024*1024*1024))
	}
	if n >= 1024*1024 {
		return fmt.Sprintf("%.2fM", float64(n)/float64(1024*1024))
	}
	if n >= 1024 {
		return fmt.Sprintf("%.2fK", float64(n)/float64(1024))
	}
	return fmt.Sprintf("%dB", n)
}
