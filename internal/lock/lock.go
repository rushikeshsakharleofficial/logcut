package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func Acquire(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another logcut process may be running for this file: %w", err)
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(fmt.Sprintf("%d\n", os.Getpid())), 0)
	_ = f.Sync()
	return f, nil
}

func ReadPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid lock file format: %w", err)
	}
	return pid, nil
}

// IsProcessAlive checks whether a process with the given PID is running.
// Uses signal(0); returns false for PIDs owned by other users (EPERM counts as not alive).
func IsProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// ForceUnlock removes a lock file if the owning process is dead.
// Returns the PID that held the lock, whether the process was found dead, and any error.
func ForceUnlock(path string) (int, bool, error) {
	pid, err := ReadPID(path)
	if err != nil {
		return 0, false, fmt.Errorf("cannot read lock file %s: %w", path, err)
	}
	if IsProcessAlive(pid) {
		return pid, false, nil
	}
	if err := os.Remove(path); err != nil {
		return pid, true, fmt.Errorf("dead process %d but cannot remove lock: %w", pid, err)
	}
	return pid, true, nil
}
