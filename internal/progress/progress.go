package progress

import (
	"fmt"
	"io"
	"time"

	"github.com/rushikeshsakharleofficial/logcut/internal/human"
)

type Reporter struct {
	Out          io.Writer
	Total        int64
	StartOffset  int64
	StartedAt    time.Time
	LastPrinted  time.Time
	Interval     time.Duration
	Quiet        bool
	Verbose      bool
}

type Snapshot struct {
	Chunk         int
	Offset        int64
	RawBytes      int64
	ArchivedBytes int64
	FreeBefore    int64
	FreeAfter     int64
	NextChunkSize int64
	Ratio         float64
	ChunkDuration time.Duration
}

func New(out io.Writer, total int64, startOffset int64, interval time.Duration, quiet bool, verbose bool) *Reporter {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	now := time.Now()
	return &Reporter{Out: out, Total: total, StartOffset: startOffset, StartedAt: now, LastPrinted: now, Interval: interval, Quiet: quiet, Verbose: verbose}
}

func (r *Reporter) Start() {
	if r.Quiet || r.Out == nil {
		return
	}
	fmt.Fprintf(r.Out, "[%s] progress: starting total=%s already_done=%s remaining=%s\n",
		timestamp(), human.FormatBytes(r.Total), human.FormatBytes(r.StartOffset), human.FormatBytes(max64(r.Total-r.StartOffset, 0)))
}

func (r *Reporter) Chunk(s Snapshot) {
	if r.Quiet || r.Out == nil {
		return
	}
	now := time.Now()
	shouldPrintSummary := now.Sub(r.LastPrinted) >= r.Interval || s.Offset >= r.Total
	if r.Verbose {
		recovered := s.FreeAfter - s.FreeBefore
		fmt.Fprintf(r.Out, "[%s] verbose: chunk=%d status=done raw=%s archived=%s punched=%s ratio=%.2f%% chunk_time=%s free_before=%s free_after=%s recovered=%s next_chunk=%s\n",
			timestamp(), s.Chunk, human.FormatBytes(s.RawBytes), human.FormatBytes(s.ArchivedBytes), human.FormatBytes(s.RawBytes), s.Ratio*100,
			s.ChunkDuration.Round(time.Millisecond), human.FormatBytes(s.FreeBefore), human.FormatBytes(s.FreeAfter), signedBytes(recovered), human.FormatBytes(s.NextChunkSize))
	}
	if shouldPrintSummary {
		r.Summary(s.Offset)
		r.LastPrinted = now
	}
}

func (r *Reporter) Summary(offset int64) {
	if r.Quiet || r.Out == nil {
		return
	}
	done := max64(offset, 0)
	if done > r.Total {
		done = r.Total
	}
	remaining := max64(r.Total-done, 0)
	percent := 0.0
	if r.Total > 0 {
		percent = float64(done) * 100 / float64(r.Total)
	}
	elapsed := time.Since(r.StartedAt)
	processedThisRun := max64(done-r.StartOffset, 0)
	speed := float64(0)
	if elapsed.Seconds() > 0 {
		speed = float64(processedThisRun) / elapsed.Seconds()
	}
	eta := "unknown"
	if speed > 0 && remaining > 0 {
		eta = time.Duration(float64(remaining) / speed * float64(time.Second)).Round(time.Second).String()
	} else if remaining == 0 {
		eta = "0s"
	}
	fmt.Fprintf(r.Out, "[%s] progress: %.2f%% done=%s remaining=%s speed=%s/s elapsed=%s eta=%s\n",
		timestamp(), percent, human.FormatBytes(done), human.FormatBytes(remaining), human.FormatBytes(int64(speed)), elapsed.Round(time.Second), eta)
}

func (r *Reporter) Complete(offset int64) {
	if r.Quiet || r.Out == nil {
		return
	}
	r.Summary(offset)
	fmt.Fprintf(r.Out, "[%s] progress: finished\n", timestamp())
}

func timestamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func signedBytes(n int64) string {
	if n >= 0 {
		return "+" + human.FormatBytes(n)
	}
	return "-" + human.FormatBytes(-n)
}
