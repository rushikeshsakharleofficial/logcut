package emergency

import (
	"fmt"
	"os"
	"time"
)

// State records forensics about why logcut had to abort.
type State struct {
	Source             string
	Output             string
	LastPunchedOffset  int64
	LastArchivedOffset int64
	CurrentChunkOffset int64
	ChunkNo            int
	Reason             string
	Timestamp          time.Time
}

// Write saves an emergency state file with a .emergency suffix.
// Unlike regular state saves, this does not use atomic rename — it writes
// directly so that it works even when the filesystem is under extreme stress.
func Write(path string, s *State) error {
	data := fmt.Sprintf(
		"source=%s\noutput=%s\nlast_archived_offset=%d\nlast_punched_offset=%d\ncurrent_chunk_offset=%d\nchunk_no=%d\nreason=%s\ntimestamp=%s\n",
		s.Source,
		s.Output,
		s.LastArchivedOffset,
		s.LastPunchedOffset,
		s.CurrentChunkOffset,
		s.ChunkNo,
		s.Reason,
		s.Timestamp.Format(time.RFC3339),
	)
	emergencyPath := path + ".emergency"
	return os.WriteFile(emergencyPath, []byte(data), 0644)
}
