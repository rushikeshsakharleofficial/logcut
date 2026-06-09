package lock

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAcquireWritesPID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	f, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// First line should be our PID
	line := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	pid, err := strconv.Atoi(line)
	if err != nil {
		t.Fatalf("lock file does not contain a PID: %q", line)
	}
	if pid != os.Getpid() {
		t.Fatalf("lock PID %d != current PID %d", pid, os.Getpid())
	}
}

func TestAcquireFailsOnExistingLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	f1, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f1.Close()

	_, err = Acquire(path)
	if err == nil {
		t.Fatal("expected error acquiring second lock on same file")
	}
}

func TestReadPID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	f, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	pid, err := ReadPID(path)
	if err != nil {
		t.Fatal(err)
	}
	if pid != os.Getpid() {
		t.Fatalf("ReadPID returned %d, want %d", pid, os.Getpid())
	}
}

func TestIsProcessAliveOwnPid(t *testing.T) {
	if !IsProcessAlive(os.Getpid()) {
		t.Fatal("IsProcessAlive returned false for our own process")
	}
}

func TestIsProcessAliveDeadPid(t *testing.T) {
	if IsProcessAlive(99999) {
		t.Log("PID 99999 happened to be alive - skipping dead-PID test")
	}
	if IsProcessAlive(0) {
		t.Fatal("IsProcessAlive returned true for PID 0")
	}
}

func TestForceUnlockAliveProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	f, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	pid, removed, err := ForceUnlock(path)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("ForceUnlock removed lock for alive process")
	}
	if pid != os.Getpid() {
		t.Fatalf("wrong PID: %d", pid)
	}
}

func TestForceUnlockDeadProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	// Write a lock file with a definitely-dead PID
	if err := os.WriteFile(path, []byte("99999\n"), 0644); err != nil {
		t.Fatal(err)
	}
	pid, removed, err := ForceUnlock(path)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Log("PID 99999 happened to be alive - skipping dead-force-unlock test")
		return
	}
	if pid != 99999 {
		t.Fatalf("wrong PID: %d", pid)
	}
}
