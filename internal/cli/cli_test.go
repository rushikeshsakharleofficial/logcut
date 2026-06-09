package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAcceptsDefaultCompressLevel(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.log")
	output := filepath.Join(dir, "archive.log.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")
	if err := os.WriteFile(source, []byte("old\nold\nold\nnew\n"), 0644); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{
		"--dry-run",
		"-g",
		"--state-dir", stateDir,
		"--lock-dir", lockDir,
		"-k", "4",
		source,
		output,
	})
	if code != 0 {
		t.Fatalf("Run returned %d, want 0", code)
	}
}

func TestRunAcceptsSmallMemoryMachineCommand(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.log")
	output := filepath.Join(dir, "archive.log.gz")
	runLog := filepath.Join(dir, "logcut-run.log")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")
	if err := os.WriteFile(source, []byte("old\nold\nold\nnew\n"), 0644); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{
		"--dry-run",
		"-v",
		"--log-file", runLog,
		"--stop-free-above", "20G",
		"--max-runtime", "30m",
		"--rate-limit", "25M",
		"--sleep-between-chunks", "2s",
		"--compress-level", "1",
		"--verify", "none",
		"-p", "5",
		"-g",
		"-k", "4",
		"--state-dir", stateDir,
		"--lock-dir", lockDir,
		source,
		output,
	})
	if code != 0 {
		t.Fatalf("Run returned %d, want 0", code)
	}

	got, err := os.ReadFile(runLog)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{
		"Work budget:       5% of free space",
		"Rate limit:        25.00M/s",
		"Sleep per chunk:   2s",
		"Verify mode:       none",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("run log does not contain %q:\n%s", want, text)
		}
	}
}
