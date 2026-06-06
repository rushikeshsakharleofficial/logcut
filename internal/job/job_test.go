package job

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestIDStable(t *testing.T) {
	a := ID("/var/log/app.log", "/tmp/app.log.gz")
	b := ID("/var/log/app.log", "/tmp/app.log.gz")
	if a != b {
		t.Fatalf("ID not stable: %s != %s", a, b)
	}
	if len(a) != 16 {
		t.Fatalf("ID length=%d want 16", len(a))
	}
}

func TestStateAndLockPaths(t *testing.T) {
	statePath := StatePath("/var/lib/logcut", "/a", "/b")
	lockPath := LockPath("/var/lock", "/a", "/b")
	if !strings.HasPrefix(statePath, filepath.Clean("/var/lib/logcut")) {
		t.Fatalf("unexpected state path: %s", statePath)
	}
	if !strings.HasPrefix(lockPath, filepath.Clean("/var/lock")) {
		t.Fatalf("unexpected lock path: %s", lockPath)
	}
}
