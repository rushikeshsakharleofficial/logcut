package compact

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rushikeshsakharleofficial/logcut/internal/state"
)

func TestBumpActivity(t *testing.T) {
	var lastActivity atomic.Int64
	bumpActivity(&lastActivity)
	if lastActivity.Load() == 0 {
		t.Fatal("bumpActivity did not update timestamp")
	}
	before := lastActivity.Load()
	time.Sleep(1 * time.Millisecond)
	bumpActivity(&lastActivity)
	if lastActivity.Load() == before {
		t.Fatal("bumpActivity did not advance timestamp")
	}
}

func TestWriteEmergency(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "job.state")
	jobState := &state.State{
		Source:             "/var/log/app.log",
		Output:             "/tmp/app.log.gz",
		LastPunchedOffset:  400,
		LastArchivedOffset: 500,
	}
	writeEmergency("punch_hole_failed", statePath, jobState, 3, 400, os.ErrDeadlineExceeded)

	data, err := os.ReadFile(statePath + ".emergency")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "last_punched_offset=400") {
		t.Fatalf("missing last_punched_offset: %s", text)
	}
	if !strings.Contains(text, "punch_hole_failed") {
		t.Fatalf("missing reason: %s", text)
	}
	if !strings.Contains(text, "chunk_no=3") {
		t.Fatalf("missing chunk_no: %s", text)
	}
}

func TestStartWatchdogDoesNotFireWhenActive(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "job.state")

	cfg := DefaultConfig()
	cfg.ChunkTimeout = 100 * time.Millisecond

	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().UnixNano())

	jobState := &state.State{
		Source:             "/var/log/app.log",
		Output:             "/tmp/app.log.gz",
		LastPunchedOffset:  0,
		LastArchivedOffset: 0,
	}
	var chunkNo, offset atomic.Int64
	stopper := &testStopper{}

	var buf bytes.Buffer
	startWatchdog(cfg, &buf, &lastActivity, statePath, jobState, &chunkNo, &offset, stopper)

	// Continuously bump activity for 3x the chunk timeout, then stop stopper
	go func() {
		for i := 0; i < 10; i++ {
			bumpActivity(&lastActivity)
			time.Sleep(30 * time.Millisecond)
		}
		stopper.requested.Store(true)
	}()

	// Wait long enough for the continuous bumping to keep watchdog at bay
	time.Sleep(400 * time.Millisecond)

	// No emergency file — watchdog should have always seen fresh activity
	if _, err := os.Stat(statePath + ".emergency"); err == nil {
		t.Fatal("emergency file written even though activity was bumped")
	}
}

type testStopper struct {
	requested atomic.Bool
}

func (s *testStopper) Requested() bool { return s.requested.Load() }
func (s *testStopper) Reason() string  { return "test" }
func (s *testStopper) Stop()           {}
