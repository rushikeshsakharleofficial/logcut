package adaptive

import (
	"fmt"
	"runtime"
	"time"

	"github.com/rushikeshsakharleofficial/logcut/internal/disk"
	"github.com/rushikeshsakharleofficial/logcut/internal/human"
)

type Snapshot struct {
	CPUs               int
	FreeBytes          int64
	Pressure           string
	RateLimitBytes     int64
	SleepBetweenChunks time.Duration
	MaxChunkBytes      int64
	CompressLevel      int
	Reason             string
}

func Evaluate(targetPath string) Snapshot {
	free, _ := disk.FreeBytes(targetPath)
	pressure := "low"
	rate := int64(75 * human.MiB)
	sleep := 500 * time.Millisecond
	maxChunk := int64(256 * human.MiB)
	reason := "normal disk headroom"
	if free < 512*human.MiB {
		pressure = "critical"
		rate = 10 * human.MiB
		sleep = 3 * time.Second
		maxChunk = 32 * human.MiB
		reason = "critical free disk headroom"
	} else if free < 2*human.GiB {
		pressure = "high"
		rate = 25 * human.MiB
		sleep = 2 * time.Second
		maxChunk = 64 * human.MiB
		reason = "low free disk headroom"
	} else if free < 5*human.GiB {
		pressure = "medium"
		rate = 50 * human.MiB
		sleep = time.Second
		maxChunk = 128 * human.MiB
		reason = "moderate free disk headroom"
	}
	return Snapshot{CPUs: runtime.NumCPU(), FreeBytes: free, Pressure: pressure, RateLimitBytes: rate, SleepBetweenChunks: sleep, MaxChunkBytes: maxChunk, CompressLevel: 1, Reason: reason}
}

func (s Snapshot) Summary() string {
	return fmt.Sprintf("pressure=%s free=%s rate=%s/s sleep=%s max_chunk=%s reason=%s", s.Pressure, human.FormatBytes(s.FreeBytes), human.FormatBytes(s.RateLimitBytes), s.SleepBetweenChunks, human.FormatBytes(s.MaxChunkBytes), s.Reason)
}
