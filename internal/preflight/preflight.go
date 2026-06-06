package preflight

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rushikeshsakharleofficial/logcut/internal/disk"
	"github.com/rushikeshsakharleofficial/logcut/internal/human"
	"github.com/rushikeshsakharleofficial/logcut/internal/job"
	"github.com/rushikeshsakharleofficial/logcut/internal/state"
)

type Config struct {
	Source   string
	Output   string
	StateDir string
	LockDir  string
	Gzip     bool
	KeepRaw  string
}

type Check struct {
	Name   string
	Status string
	Detail string
}

type Result struct {
	Checks []Check
}

func (r *Result) add(status string, name string, detail string) {
	r.Checks = append(r.Checks, Check{Name: name, Status: status, Detail: detail})
}

func (r Result) Failed() bool {
	for _, c := range r.Checks {
		if c.Status == "FAIL" {
			return true
		}
	}
	return false
}

func Run(cfg Config) Result {
	res := Result{}
	absSrc, _ := filepath.Abs(cfg.Source)
	absOut, _ := filepath.Abs(cfg.Output)
	cfg.Source = absSrc
	cfg.Output = absOut

	if cfg.Source == cfg.Output {
		res.add("FAIL", "source-output", "source and output are the same path")
	} else {
		res.add("PASS", "source-output", "source and output paths differ")
	}

	st, err := os.Lstat(cfg.Source)
	if err != nil {
		res.add("FAIL", "source-stat", err.Error())
	} else {
		if st.Mode()&os.ModeSymlink != 0 {
			res.add("FAIL", "source-symlink", "source is a symlink; refusing by default")
		} else if !st.Mode().IsRegular() {
			res.add("FAIL", "source-regular", "source is not a regular file")
		} else {
			res.add("PASS", "source-regular", "source is a regular file")
		}
	}

	info, err := disk.FileInfo(cfg.Source)
	if err != nil {
		res.add("FAIL", "source-info", err.Error())
	} else {
		res.add("PASS", "source-info", fmt.Sprintf("size=%s real_usage=%s inode=%d device=%d", human.FormatBytes(info.Size), human.FormatBytes(info.BlocksUsed), info.Inode, info.Device))
	}

	if f, err := os.OpenFile(cfg.Source, os.O_RDWR, 0); err != nil {
		res.add("FAIL", "source-open-rw", err.Error())
	} else {
		_ = f.Close()
		res.add("PASS", "source-open-rw", "source can be opened read-write")
	}

	if err := disk.TestPunchHole(filepath.Dir(cfg.Source)); err != nil {
		res.add("FAIL", "punch-hole", err.Error())
	} else {
		res.add("PASS", "punch-hole", "filesystem accepted punch-hole test")
	}

	outDir := filepath.Dir(cfg.Output)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		res.add("FAIL", "output-dir", err.Error())
	} else if f, err := os.CreateTemp(outDir, ".logcut-write-test-"); err != nil {
		res.add("FAIL", "output-writable", err.Error())
	} else {
		_, _ = f.Write([]byte("test"))
		_ = f.Close()
		_ = os.Remove(f.Name())
		res.add("PASS", "output-writable", "output directory is writable")
	}

	for _, dir := range []struct{ name, path string }{{"state-dir", cfg.StateDir}, {"lock-dir", cfg.LockDir}} {
		if err := os.MkdirAll(dir.path, 0755); err != nil {
			res.add("FAIL", dir.name, err.Error())
		} else if f, err := os.CreateTemp(dir.path, ".logcut-write-test-"); err != nil {
			res.add("FAIL", dir.name, err.Error())
		} else {
			_ = f.Close()
			_ = os.Remove(f.Name())
			res.add("PASS", dir.name, dir.path+" is writable")
		}
	}

	if free, err := disk.FreeBytes(outDir); err != nil {
		res.add("FAIL", "free-space", err.Error())
	} else {
		res.add("PASS", "free-space", human.FormatBytes(free)+" available")
	}

	statePath := job.StatePath(cfg.StateDir, cfg.Source, cfg.Output)
	if s, err := state.Load(statePath); err == nil {
		res.add("WARN", "existing-state", fmt.Sprintf("state exists last_archived=%s last_punched=%s", human.FormatBytes(s.LastArchivedOffset), human.FormatBytes(s.LastPunchedOffset)))
	} else if os.IsNotExist(err) {
		res.add("PASS", "existing-state", "no previous state file")
	} else {
		res.add("WARN", "existing-state", err.Error())
	}

	return res
}

func Print(w io.Writer, r Result) {
	for _, c := range r.Checks {
		fmt.Fprintf(w, "[%s] %s: %s\n", c.Status, c.Name, c.Detail)
	}
	if r.Failed() {
		fmt.Fprintln(w, "Preflight result: FAIL")
	} else {
		fmt.Fprintln(w, "Preflight result: PASS")
	}
}
