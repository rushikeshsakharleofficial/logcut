package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type State struct {
	Source             string
	Output             string
	Inode              uint64
	Device             uint64
	OriginalSize       int64
	CutoffOffset       int64
	LastPunchedOffset  int64
	LastArchivedOffset int64
	Gzip               bool
	UpdatedAt          string
}

func Load(path string) (*State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := &State{}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		k, v := parts[0], parts[1]
		switch k {
		case "source":
			s.Source = v
		case "output":
			s.Output = v
		case "inode":
			s.Inode, _ = strconv.ParseUint(v, 10, 64)
		case "device":
			s.Device, _ = strconv.ParseUint(v, 10, 64)
		case "original_size":
			s.OriginalSize, _ = strconv.ParseInt(v, 10, 64)
		case "cutoff_offset":
			s.CutoffOffset, _ = strconv.ParseInt(v, 10, 64)
		case "last_punched_offset":
			s.LastPunchedOffset, _ = strconv.ParseInt(v, 10, 64)
		case "last_archived_offset":
			s.LastArchivedOffset, _ = strconv.ParseInt(v, 10, 64)
		case "gzip":
			s.Gzip = v == "true"
		case "updated_at":
			s.UpdatedAt = v
		}
	}
	return s, nil
}

func Save(path string, s *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	data := fmt.Sprintf("source=%s\noutput=%s\ninode=%d\ndevice=%d\noriginal_size=%d\ncutoff_offset=%d\nlast_archived_offset=%d\nlast_punched_offset=%d\ngzip=%t\nupdated_at=%s\n",
		s.Source, s.Output, s.Inode, s.Device, s.OriginalSize, s.CutoffOffset, s.LastArchivedOffset, s.LastPunchedOffset, s.Gzip, time.Now().Format(time.RFC3339))
	if err := os.WriteFile(tmp, []byte(data), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
