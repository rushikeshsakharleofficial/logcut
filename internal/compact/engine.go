package compact

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rushikeshsakharleofficial/logcut/internal/adaptive"
	"github.com/rushikeshsakharleofficial/logcut/internal/control"
	"github.com/rushikeshsakharleofficial/logcut/internal/disk"
	"github.com/rushikeshsakharleofficial/logcut/internal/emergency"
	"github.com/rushikeshsakharleofficial/logcut/internal/event"
	"github.com/rushikeshsakharleofficial/logcut/internal/human"
	"github.com/rushikeshsakharleofficial/logcut/internal/job"
	"github.com/rushikeshsakharleofficial/logcut/internal/lock"
	"github.com/rushikeshsakharleofficial/logcut/internal/progress"
	"github.com/rushikeshsakharleofficial/logcut/internal/state"
)

type Config struct {
	GzipOutput         bool
	KeepLastRaw        string
	WorkingPercent     int64
	DryRun             bool
	Force              bool
	Quiet              bool
	Verbose            bool
	JSON               bool
	AutoThrottle       bool
	ProgressInterval   time.Duration
	StopFreeAboveRaw   string
	StopFreeAbove      int64
	MaxRuntime         time.Duration
	ChunkTimeout       time.Duration
	RateLimitRaw       string
	RateLimitBytes     int64
	SleepBetweenChunks time.Duration
	LogFile            string
	CompressLevel      int
	VerifyMode         string
	Source             string
	Output             string
	KeepLastBytes      int64
	MinChunk           int64
	MaxChunk           int64
	SampleSize         int64
	StateDir           string
	LockDir            string
}

func DefaultConfig() Config {
	return Config{
		WorkingPercent:   20,
		AutoThrottle:     true,
		ProgressInterval: 5 * time.Second,
		CompressLevel:    0,
		VerifyMode:       "full",
		MinChunk:         8 * human.MiB,
		MaxChunk:         512 * human.MiB,
		SampleSize:       32 * human.MiB,
		StateDir:         "/var/lib/logcut",
		LockDir:          "/var/lock",
		ChunkTimeout:     5 * time.Minute,
	}
}

func Run(cfg Config) error {
	startedAt := time.Now()
	out, closeOut, err := outputWriter(cfg)
	if err != nil {
		return err
	}
	defer closeOut()
	events := event.Writer{Out: out, Enabled: cfg.JSON}
	stopper := control.NewStopper()
	defer stopper.Stop()

	absSrc, _ := filepath.Abs(cfg.Source)
	absOut, _ := filepath.Abs(cfg.Output)
	cfg.Source = absSrc
	cfg.Output = absOut

	manualRate := cfg.RateLimitRaw != "" || cfg.RateLimitBytes > 0
	manualSleep := cfg.SleepBetweenChunks > 0
	if cfg.RateLimitRaw == "" {
		cfg.RateLimitRaw = strings.TrimSpace(os.Getenv("LOGCUT_RATE_LIMIT"))
		manualRate = cfg.RateLimitRaw != "" || cfg.RateLimitBytes > 0
	}
	if cfg.SleepBetweenChunks == 0 {
		if v := strings.TrimSpace(os.Getenv("LOGCUT_SLEEP_BETWEEN_CHUNKS")); v != "" {
			if d, e := time.ParseDuration(v); e == nil {
				cfg.SleepBetweenChunks = d
				manualSleep = true
			}
		}
	}

	vlogf(out, cfg, "starting source=%q output=%q gzip=%t dry_run=%t auto=%t", cfg.Source, cfg.Output, cfg.GzipOutput, cfg.DryRun, cfg.AutoThrottle)
	events.Emit("start", map[string]interface{}{"source": cfg.Source, "output": cfg.Output, "gzip": cfg.GzipOutput, "auto": cfg.AutoThrottle})

	if cfg.StopFreeAboveRaw != "" {
		cfg.StopFreeAbove, err = human.ParseSize(cfg.StopFreeAboveRaw)
		if err != nil {
			return fmt.Errorf("invalid --stop-free-above value: %w", err)
		}
	}
	if cfg.RateLimitRaw != "" {
		cfg.RateLimitBytes, err = human.ParseSize(cfg.RateLimitRaw)
		if err != nil {
			return fmt.Errorf("invalid --rate-limit value: %w", err)
		}
	}
	if cfg.VerifyMode == "" {
		cfg.VerifyMode = "full"
	}
	if cfg.VerifyMode != "full" && cfg.VerifyMode != "none" {
		return errors.New("invalid --verify value; use full or none")
	}
	if cfg.Source == cfg.Output {
		return errors.New("source and output cannot be the same file")
	}

	vlogf(out, cfg, "scanning source log")
	info, err := disk.FileInfo(cfg.Source)
	if err != nil {
		return err
	}
	if info.Size <= 0 {
		return errors.New("source log is empty")
	}

	if cfg.KeepLastRaw != "" {
		cfg.KeepLastBytes, err = human.ParseSize(cfg.KeepLastRaw)
		if err != nil {
			return fmt.Errorf("invalid keep-last value: %w", err)
		}
	} else {
		cfg.KeepLastBytes = info.Size / 10
		if cfg.KeepLastBytes < 64*human.MiB && info.Size > 640*human.MiB {
			cfg.KeepLastBytes = 64 * human.MiB
		}
	}
	if cfg.KeepLastBytes >= info.Size {
		return fmt.Errorf("keep-last %s is greater/equal to log size %s", human.FormatBytes(cfg.KeepLastBytes), human.FormatBytes(info.Size))
	}

	cutoff := info.Size - cfg.KeepLastBytes
	if cutoff <= 0 {
		return errors.New("nothing to compact")
	}

	statePath := job.StatePath(cfg.StateDir, cfg.Source, cfg.Output)
	lockPath := job.LockPath(cfg.LockDir, cfg.Source, cfg.Output)
	vlogf(out, cfg, "acquiring lock path=%q", lockPath)
	lk, err := lock.Acquire(lockPath)
	if err != nil {
		return err
	}
	defer lk.Close()

	outDir := filepath.Dir(cfg.Output)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	vlogf(out, cfg, "checking punch-hole support")
	if err := disk.TestPunchHole(filepath.Dir(cfg.Source)); err != nil {
		return fmt.Errorf("filesystem does not support punch-hole on source path or permission denied: %w", err)
	}

	src, err := os.OpenFile(cfg.Source, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer src.Close()

	free, err := disk.FreeBytes(outDir)
	if err != nil {
		return err
	}

	if cfg.AutoThrottle {
		snap := adaptive.Evaluate(outDir)
		if !manualRate {
			cfg.RateLimitBytes = snap.RateLimitBytes
		}
		if !manualSleep {
			cfg.SleepBetweenChunks = snap.SleepBetweenChunks
		}
		if cfg.CompressLevel == 0 {
			cfg.CompressLevel = snap.CompressLevel
		}
		if snap.MaxChunkBytes > 0 && snap.MaxChunkBytes < cfg.MaxChunk {
			cfg.MaxChunk = snap.MaxChunkBytes
		}
		infof(out, cfg, "auto-throttle: %s", snap.Summary())
		events.Emit("auto_throttle", map[string]interface{}{"pressure": snap.Pressure, "free_bytes": snap.FreeBytes, "rate_limit": snap.RateLimitBytes, "sleep_ms": snap.SleepBetweenChunks.Milliseconds(), "max_chunk": snap.MaxChunkBytes})
	}
	if cfg.CompressLevel == 0 {
		cfg.CompressLevel = gzip.DefaultCompression
	}

	vlogf(out, cfg, "sampling compression ratio sample_size=%s", human.FormatBytes(cfg.SampleSize))
	ratio, err := estimateCompressionRatio(src, 0, cfg.SampleSize, cfg.GzipOutput, cfg.CompressLevel)
	if err != nil {
		return fmt.Errorf("compression sample failed: %w", err)
	}
	if !cfg.GzipOutput && !cfg.Force && free < 2*human.GiB {
		return errors.New("plain mode on low disk is risky; use -g for gzip output or pass --force")
	}

	initialChunk := chooseChunkSize(free, cfg.WorkingPercent, ratio, cfg.MinChunk, cfg.MaxChunk)
	printPlan(out, cfg, info, cutoff, free, ratio, initialChunk, statePath)
	events.Emit("plan", map[string]interface{}{"source_size": info.Size, "rotate_bytes": cutoff, "free_bytes": free, "initial_chunk": initialChunk, "state_path": statePath})
	if initialChunk <= 0 {
		return errors.New("not enough free space even with minimum chunk")
	}
	if cfg.DryRun {
		infof(out, cfg, "dry-run complete; no files changed")
		events.Emit("complete", map[string]interface{}{"dry_run": true})
		return nil
	}

	jobState := &state.State{Source: cfg.Source, Output: cfg.Output, Inode: info.Inode, Device: info.Device, OriginalSize: info.Size, CutoffOffset: cutoff, Gzip: cfg.GzipOutput}
	if old, err := state.Load(statePath); err == nil {
		if old.Source == cfg.Source && old.Output == cfg.Output && old.Inode == info.Inode && old.Device == info.Device && old.CutoffOffset == cutoff && old.Gzip == cfg.GzipOutput {
			jobState = old
			infof(out, cfg, "resuming from state offset=%s", human.FormatBytes(jobState.LastPunchedOffset))
		} else {
			return errors.New("existing state file does not match this job; remove it manually if you are sure")
		}
	}
	offset := jobState.LastPunchedOffset
	if offset < 0 || offset > cutoff {
		return errors.New("invalid state offset")
	}

	reporter := progress.New(out, cutoff, offset, cfg.ProgressInterval, cfg.Quiet || cfg.JSON, cfg.Verbose)
	reporter.Start()
	chunkNo := 0
	lastRatio := ratio
	stopReason := "completed"

	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().UnixNano())
	var watchdogChunk, watchdogOffset atomic.Int64
	watchdogDone := startWatchdog(cfg, out, &lastActivity, statePath, jobState, &watchdogChunk, &watchdogOffset, stopper)

	for offset < cutoff {
		bumpActivity(&lastActivity)
		select {
		case <-watchdogDone:
			return fmt.Errorf("watchdog: chunk timeout exceeded; see %s.emergency", statePath)
		default:
		}
		if stopper.Requested() {
			stopReason = "signal-" + stopper.Reason()
			infof(out, cfg, "safe stop requested: %s", stopper.Reason())
			break
		}
		if cfg.MaxRuntime > 0 && time.Since(startedAt) >= cfg.MaxRuntime {
			stopReason = "max-runtime"
			infof(out, cfg, "safe stop requested: max runtime reached")
			break
		}
		freeBefore, err := disk.FreeBytes(outDir)
		if err != nil {
			return err
		}
		if cfg.AutoThrottle {
			snap := adaptive.Evaluate(outDir)
			if !manualRate {
				cfg.RateLimitBytes = snap.RateLimitBytes
			}
			if !manualSleep {
				cfg.SleepBetweenChunks = snap.SleepBetweenChunks
			}
			if snap.MaxChunkBytes > 0 {
				cfg.MaxChunk = snap.MaxChunkBytes
			}
		}
		if cfg.StopFreeAbove > 0 && freeBefore >= cfg.StopFreeAbove {
			stopReason = "stop-free-above"
			infof(out, cfg, "safe stop requested: free space %s reached target %s", human.FormatBytes(freeBefore), human.FormatBytes(cfg.StopFreeAbove))
			break
		}
		chunkNo++
		watchdogChunk.Store(int64(chunkNo))
		chunkSize := chooseChunkSize(freeBefore, cfg.WorkingPercent, lastRatio, cfg.MinChunk, cfg.MaxChunk)
		if chunkSize <= 0 {
			return fmt.Errorf("free space too low for next safe chunk; current free=%s", human.FormatBytes(freeBefore))
		}
		if offset+chunkSize > cutoff {
			chunkSize = cutoff - offset
		}
		chunkStarted := time.Now()
		vlogf(out, cfg, "chunk=%d status=read offset=%s target_chunk=%s", chunkNo, human.FormatBytes(offset), human.FormatBytes(chunkSize))
		events.Emit("chunk_start", map[string]interface{}{"chunk": chunkNo, "offset": offset, "target_chunk": chunkSize})
		vlogf(out, cfg, "chunk=%d status=archive", chunkNo)
		bumpActivity(&lastActivity)
		end, raw, written, err := appendLineSafeChunk(cfg.Output, src, offset, chunkSize, cutoff, cfg.GzipOutput, cfg.CompressLevel)
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("archive append failed at offset %d: %w", offset, err)
		}
		if raw == 0 || end <= offset {
			break
		}
		if written <= 0 {
			return errors.New("archive append wrote zero bytes; refusing to punch")
		}
		jobState.LastArchivedOffset = end
		if err := state.Save(statePath, jobState); err != nil {
			return err
		}
		vlogf(out, cfg, "chunk=%d status=punch offset=%s length=%s", chunkNo, human.FormatBytes(offset), human.FormatBytes(end-offset))
		bumpActivity(&lastActivity)
		if err := disk.PunchHole(src, offset, end-offset); err != nil {
			writeEmergency("punch_hole_failed", statePath, jobState, chunkNo, offset, err)
			return fmt.Errorf("punch-hole failed at offset %d length %d: %w", offset, end-offset, err)
		}
		bumpActivity(&lastActivity)
		if err := src.Sync(); err != nil {
			writeEmergency("sync_failed", statePath, jobState, chunkNo, offset, err)
			return err
		}
		bumpActivity(&lastActivity)
		jobState.LastPunchedOffset = end
		if err := state.Save(statePath, jobState); err != nil {
			return err
		}
		if raw > 0 {
			lastRatio = float64(written) / float64(raw)
			if lastRatio < 0.03 {
				lastRatio = 0.03
			}
			if lastRatio > 1.15 {
				lastRatio = 1.15
			}
		}
		offset = end
		watchdogOffset.Store(offset)
		freeAfter, _ := disk.FreeBytes(outDir)
		nextChunkSize := chooseChunkSize(freeAfter, cfg.WorkingPercent, lastRatio, cfg.MinChunk, cfg.MaxChunk)
		duration := time.Since(chunkStarted)
		reporter.Chunk(progress.Snapshot{Chunk: chunkNo, Offset: offset, RawBytes: raw, ArchivedBytes: written, FreeBefore: freeBefore, FreeAfter: freeAfter, NextChunkSize: nextChunkSize, Ratio: lastRatio, ChunkDuration: duration})
		events.Emit("chunk_done", map[string]interface{}{"chunk": chunkNo, "offset": offset, "raw_bytes": raw, "archived_bytes": written, "free_before": freeBefore, "free_after": freeAfter, "duration_ms": duration.Milliseconds(), "ratio": lastRatio})
		applyPacing(cfg, raw, duration)
	}
	if cfg.GzipOutput && cfg.VerifyMode == "full" && stopReason == "completed" {
		vlogf(out, cfg, "verifying gzip archive path=%q", cfg.Output)
		if err := verifyGzip(cfg.Output); err != nil {
			return fmt.Errorf("gzip verification failed: %w", err)
		}
	} else if cfg.GzipOutput && cfg.VerifyMode == "none" {
		infof(out, cfg, "gzip verification skipped (--verify none)")
	}
	finalInfo, _ := disk.FileInfo(cfg.Source)
	reporter.Complete(offset)
	if !cfg.Quiet && !cfg.JSON {
		fmt.Fprintln(out, "Complete.")
		fmt.Fprintln(out, "  Stop reason:              ", stopReason)
		fmt.Fprintln(out, "  Active log apparent size:", human.FormatBytes(finalInfo.Size))
		fmt.Fprintln(out, "  Active log real usage:   ", human.FormatBytes(finalInfo.BlocksUsed))
		if st, err := os.Stat(cfg.Output); err == nil {
			fmt.Fprintln(out, "  Rotated output size:     ", human.FormatBytes(st.Size()))
		}
		fmt.Fprintln(out, "  Total runtime:           ", time.Since(startedAt).Round(time.Second))
		fmt.Fprintln(out, "  Check real usage with: du -h", cfg.Source)
	}
	events.Emit("complete", map[string]interface{}{"stop_reason": stopReason, "offset": offset, "runtime_ms": time.Since(startedAt).Milliseconds(), "active_real_usage": finalInfo.BlocksUsed})
	return nil
}

func applyPacing(cfg Config, raw int64, duration time.Duration) {
	if cfg.RateLimitBytes > 0 && raw > 0 {
		minDuration := time.Duration(float64(raw) / float64(cfg.RateLimitBytes) * float64(time.Second))
		if minDuration > duration {
			time.Sleep(minDuration - duration)
		}
	}
	if cfg.SleepBetweenChunks > 0 {
		time.Sleep(cfg.SleepBetweenChunks)
	}
}

func writeEmergency(kind string, statePath string, jobState *state.State, chunkNo int, offset int64, err error) {
	em := &emergency.State{
		Source:             jobState.Source,
		Output:             jobState.Output,
		LastPunchedOffset:  jobState.LastPunchedOffset,
		LastArchivedOffset: jobState.LastArchivedOffset,
		CurrentChunkOffset: offset,
		ChunkNo:            chunkNo,
		Reason:             fmt.Sprintf("%s: %v", kind, err),
		Timestamp:          time.Now(),
	}
	_ = emergency.Write(statePath, em)
}

func startWatchdog(cfg Config, out io.Writer, lastActivity *atomic.Int64, statePath string, jobState *state.State, chunkNo, offset *atomic.Int64, stopper interface{ Requested() bool }) <-chan struct{} {
	if cfg.ChunkTimeout <= 0 {
		return nil
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Duration(float64(cfg.ChunkTimeout) * 0.5))
		defer ticker.Stop()
		for range ticker.C {
			if stopper.Requested() {
				return
			}
			last := time.Unix(0, lastActivity.Load())
			if time.Since(last) > cfg.ChunkTimeout {
				err := fmt.Errorf("watchdog: no progress for %v; aborting", time.Since(last))
				writeEmergency("watchdog_timeout", statePath, jobState, int(chunkNo.Load()), offset.Load(), err)
				fmt.Fprintf(out, "\n[%s] FATAL: %v\n", time.Now().Format("2006-01-02 15:04:05"), err)
				fmt.Fprintf(out, "Emergency state saved to %s.emergency\n", statePath)
				fmt.Fprintf(out, "Resume with: logcut -g -k <size> %s %s\n", jobState.Source, jobState.Output)
				close(done)
				return
			}
		}
	}()
	return done
}

func bumpActivity(lastActivity *atomic.Int64) {
	lastActivity.Store(time.Now().UnixNano())
}

func outputWriter(cfg Config) (io.Writer, func(), error) {
	if cfg.LogFile == "" {
		return os.Stdout, func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0755); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, nil, err
	}
	return io.MultiWriter(os.Stdout, f), func() { _ = f.Close() }, nil
}

func printPlan(w io.Writer, cfg Config, info disk.FileStats, cutoff int64, free int64, ratio float64, initialChunk int64, statePath string) {
	if cfg.Quiet || cfg.JSON {
		return
	}
	fmt.Fprintln(w, "Plan:")
	fmt.Fprintln(w, "  Source:           ", cfg.Source)
	fmt.Fprintln(w, "  Output:           ", cfg.Output)
	fmt.Fprintln(w, "  Gzip output:      ", cfg.GzipOutput)
	fmt.Fprintln(w, "  Auto throttle:    ", cfg.AutoThrottle)
	fmt.Fprintln(w, "  Verbose:          ", cfg.Verbose)
	fmt.Fprintln(w, "  Log apparent size:", human.FormatBytes(info.Size))
	fmt.Fprintln(w, "  Log real usage:   ", human.FormatBytes(info.BlocksUsed))
	fmt.Fprintln(w, "  Keep latest:      ", human.FormatBytes(cfg.KeepLastBytes))
	fmt.Fprintln(w, "  Rotate old range: ", human.FormatBytes(cutoff))
	fmt.Fprintln(w, "  Free space:       ", human.FormatBytes(free))
	fmt.Fprintln(w, "  Work budget:      ", fmt.Sprintf("%d%% of free space", cfg.WorkingPercent))
	fmt.Fprintln(w, "  Compression ratio:", fmt.Sprintf("%.2f%%", ratio*100))
	fmt.Fprintln(w, "  Initial chunk:    ", human.FormatBytes(initialChunk))
	fmt.Fprintln(w, "  Stop free above:  ", human.FormatBytes(cfg.StopFreeAbove))
	fmt.Fprintln(w, "  Max runtime:      ", cfg.MaxRuntime)
	fmt.Fprintln(w, "  Rate limit:       ", human.FormatBytes(cfg.RateLimitBytes)+"/s")
	fmt.Fprintln(w, "  Sleep per chunk:  ", cfg.SleepBetweenChunks)
	fmt.Fprintln(w, "  Compress level:   ", cfg.CompressLevel)
	fmt.Fprintln(w, "  Verify mode:      ", cfg.VerifyMode)
	fmt.Fprintln(w, "  Progress interval:", cfg.ProgressInterval)
	fmt.Fprintln(w, "  State file:       ", statePath)
}

func infof(w io.Writer, cfg Config, format string, args ...interface{}) {
	if !cfg.Quiet && !cfg.JSON {
		fmt.Fprintf(w, "[%s] "+format+"\n", append([]interface{}{time.Now().Format("2006-01-02 15:04:05")}, args...)...)
	}
}
func vlogf(w io.Writer, cfg Config, format string, args ...interface{}) {
	if !cfg.Quiet && cfg.Verbose && !cfg.JSON {
		fmt.Fprintf(w, "[%s] verbose: "+format+"\n", append([]interface{}{time.Now().Format("2006-01-02 15:04:05")}, args...)...)
	}
}

func estimateCompressionRatio(src *os.File, offset, max int64, gzipEnabled bool, level int) (float64, error) {
	if !gzipEnabled {
		return 1.0, nil
	}
	if max <= 0 {
		max = 16 * human.MiB
	}
	if _, err := src.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	lr := &io.LimitedReader{R: src, N: max}
	pr, pw := io.Pipe()
	var written int64
	var gzErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 64*1024)
		for {
			n, err := pr.Read(buf)
			if n > 0 {
				written += int64(n)
			}
			if err != nil {
				return
			}
		}
	}()
	gw, err := gzip.NewWriterLevel(pw, level)
	if err != nil {
		return 0, err
	}
	raw, err := io.Copy(gw, lr)
	if err != nil {
		gzErr = err
	}
	if err := gw.Close(); err != nil && gzErr == nil {
		gzErr = err
	}
	_ = pw.Close()
	<-done
	if gzErr != nil {
		return 0, gzErr
	}
	if raw <= 0 {
		return 1.0, nil
	}
	ratio := float64(written) / float64(raw)
	if ratio < 0.03 {
		ratio = 0.03
	}
	if ratio > 1.15 {
		ratio = 1.15
	}
	return ratio, nil
}

func chooseChunkSize(free, percent int64, ratio float64, minChunk, maxChunk int64) int64 {
	if percent <= 0 || percent > 80 {
		percent = 20
	}
	workingBudget := free * percent / 100
	safeOutputLimit := workingBudget * 70 / 100
	if safeOutputLimit < 8*human.MiB {
		return 0
	}
	raw := int64(float64(safeOutputLimit) / ratio)
	if raw < minChunk {
		raw = minChunk
	}
	if raw > maxChunk {
		raw = maxChunk
	}
	raw = raw / human.MiB * human.MiB
	if raw < minChunk {
		raw = minChunk
	}
	return raw
}

func appendLineSafeChunk(outputPath string, src *os.File, start, target, cutoff int64, gzipEnabled bool, level int) (int64, int64, int64, error) {
	if start >= cutoff {
		return start, 0, 0, io.EOF
	}
	outBefore := int64(0)
	if st, err := os.Stat(outputPath); err == nil {
		outBefore = st.Size()
	}
	out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return start, 0, 0, err
	}
	writer := io.Writer(out)
	closeWriter := func() error { return nil }
	if gzipEnabled {
		gw, err := gzip.NewWriterLevel(out, level)
		if err != nil {
			_ = out.Close()
			return start, 0, 0, err
		}
		writer = gw
		closeWriter = gw.Close
	}
	end, raw, err := writeLineSafeChunk(writer, src, start, target, cutoff)
	if closeErr := closeWriter(); err == nil && closeErr != nil {
		err = closeErr
	}
	if syncErr := out.Sync(); err == nil && syncErr != nil {
		err = syncErr
	}
	if closeErr := out.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return end, raw, 0, err
	}
	if dir, err := os.Open(filepath.Dir(outputPath)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	outAfter := int64(0)
	if st, err := os.Stat(outputPath); err == nil {
		outAfter = st.Size()
	}
	return end, raw, outAfter - outBefore, nil
}

func writeLineSafeChunk(w io.Writer, src *os.File, start, target, cutoff int64) (int64, int64, error) {
	if start >= cutoff {
		return start, 0, io.EOF
	}
	maxEnd := start + target
	if maxEnd > cutoff {
		maxEnd = cutoff
	}
	if _, err := src.Seek(start, io.SeekStart); err != nil {
		return start, 0, err
	}
	reader := bufio.NewReaderSize(src, 1024*1024)
	pos := start
	raw := int64(0)
	for pos < cutoff {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if pos+int64(len(line)) > cutoff {
				break
			}
			if _, writeErr := w.Write(line); writeErr != nil {
				return pos, raw, writeErr
			}
			pos += int64(len(line))
			raw += int64(len(line))
			if pos >= maxEnd && strings.HasSuffix(string(line), "\n") {
				break
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return pos, raw, err
		}
		if raw > target+64*human.MiB {
			break
		}
	}
	if raw == 0 {
		return start, 0, io.EOF
	}
	return pos, raw, nil
}

func verifyGzip(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()
	_, err = io.Copy(io.Discard, gr)
	return err
}
