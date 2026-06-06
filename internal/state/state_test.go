package state

import (
	"path/filepath"
	"testing"
)

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.state")
	want := &State{
		Source:             "/var/log/app.log",
		Output:             "/tmp/app.log.gz",
		Inode:              10,
		Device:             20,
		OriginalSize:       1000,
		CutoffOffset:       800,
		LastArchivedOffset: 400,
		LastPunchedOffset:  400,
		Gzip:               true,
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got.Source != want.Source || got.Output != want.Output || got.LastPunchedOffset != want.LastPunchedOffset || !got.Gzip {
		t.Fatalf("loaded state mismatch: %#v", got)
	}
}
