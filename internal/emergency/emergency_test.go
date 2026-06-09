package emergency

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "job.state")
	s := &State{
		Source:             "/var/log/app.log",
		Output:             "/tmp/app.log.gz",
		LastPunchedOffset:  800,
		LastArchivedOffset: 900,
		CurrentChunkOffset: 700,
		ChunkNo:            3,
		Reason:             "watchdog_timeout: no progress for 5m0s",
		Timestamp:          time.Now(),
	}
	if err := Write(statePath, s); err != nil {
		t.Fatal(err)
	}
	emergencyPath := statePath + ".emergency"
	data, err := os.ReadFile(emergencyPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"source=/var/log/app.log",
		"output=/tmp/app.log.gz",
		"last_punched_offset=800",
		"last_archived_offset=900",
		"current_chunk_offset=700",
		"chunk_no=3",
		"reason=watchdog_timeout: no progress for 5m0s",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("emergency state missing %q:\n%s", want, text)
		}
	}
}

func TestWriteEmptyReason(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "job.state")
	s := &State{
		Source:    "/var/log/app.log",
		Output:    "/tmp/app.log.gz",
		Timestamp: time.Now(),
	}
	if err := Write(statePath, s); err != nil {
		t.Fatal(err)
	}
	emergencyPath := statePath + ".emergency"
	if _, err := os.Stat(emergencyPath); err != nil {
		t.Fatalf("emergency file not created: %v", err)
	}
}
