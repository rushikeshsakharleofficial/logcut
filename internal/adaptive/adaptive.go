package adaptive

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rushikeshsakharleofficial/logcut/internal/disk"
	"github.com/rushikeshsakharleofficial/logcut/internal/human"
)

type Snapshot struct {
	CPUs               int
	Load1              float64
	LoadRatio          float64
	MemAvailable       int64
	MemTotal           int64
	MemAvailableRatio  float64
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
	cpus := runtime.NumCPU()
	load1 := readLoad1()
	loadRatio := 0.0
	if cpus > 0 {
		loadRatio = load1 / float64(cpus)
	}
	memAvail, memTotal := readMemInfo()
	memRatio := 1.0
	if memTotal > 0 {
		memRatio = float64(memAvail) / float64(memTotal)
	}

	pressure := "low"
	rate := int64(75 * human.MiB)
	sleep := 500 * time.Millisecond
	maxChunk := int64(256 * human.MiB)
	reason := "normal system pressure"
	if free < 512*human.MiB || loadRatio >= 1.50 || memRatio < 0.08 {
		pressure = "critical"
		rate = 10 * human.MiB
		sleep = 3 * time.Second
		maxChunk = 32 * human.MiB
		reason = "critical disk, load, or memory pressure"
	} else if free < 2*human.GiB || loadRatio >= 1.00 || memRatio < 0.15 {
		pressure = "high"
		rate = 25 * human.MiB
		sleep = 2 * time.Second
		maxChunk = 64 * human.MiB
		reason = "high disk, load, or memory pressure"
	} else if free < 5*human.GiB || loadRatio >= 0.70 || memRatio < 0.25 {
		pressure = "medium"
		rate = 50 * human.MiB
		sleep = time.Second
		maxChunk = 128 * human.MiB
		reason = "moderate disk, load, or memory pressure"
	}
	return Snapshot{CPUs: cpus, Load1: load1, LoadRatio: loadRatio, MemAvailable: memAvail, MemTotal: memTotal, MemAvailableRatio: memRatio, FreeBytes: free, Pressure: pressure, RateLimitBytes: rate, SleepBetweenChunks: sleep, MaxChunkBytes: maxChunk, CompressLevel: 1, Reason: reason}
}

func (s Snapshot) Summary() string {
	return fmt.Sprintf("pressure=%s load1=%.2f cpus=%d mem_avail=%.1f%% free=%s rate=%s/s sleep=%s max_chunk=%s reason=%s", s.Pressure, s.Load1, s.CPUs, s.MemAvailableRatio*100, human.FormatBytes(s.FreeBytes), human.FormatBytes(s.RateLimitBytes), s.SleepBetweenChunks, human.FormatBytes(s.MaxChunkBytes), s.Reason)
}

func readLoad1() float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(b))
	if len(parts) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(parts[0], 64)
	return v
}

func readMemInfo() (int64, int64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	var total int64
	var available int64
	s := bufio.NewScanner(f)
	for s.Scan() {
		parts := strings.Fields(s.Text())
		if len(parts) < 2 {
			continue
		}
		v, _ := strconv.ParseInt(parts[1], 10, 64)
		switch parts[0] {
		case "MemTotal:":
			total = v * 1024
		case "MemAvailable:":
			available = v * 1024
		}
	}
	return available, total
}
