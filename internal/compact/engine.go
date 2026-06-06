package compact

import (
	"bufio"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rushikeshsakharleofficial/logcut/internal/disk"
	"github.com/rushikeshsakharleofficial/logcut/internal/human"
	"github.com/rushikeshsakharleofficial/logcut/internal/lock"
	"github.com/rushikeshsakharleofficial/logcut/internal/progress"
	"github.com/rushikeshsakharleofficial/logcut/internal/state"
)

type Config struct {
	GzipOutput       bool
	KeepLastRaw      string
	WorkingPercent   int64
	DryRun           bool
	Force            bool
	Quiet            bool
	Verbose          bool
	ProgressInterval time.Duration
	Source           string
	Output           string
	KeepLastBytes    int64
	MinChunk         int64
	MaxChunk         int64
	SampleSize       int64
	StateDir         string
	LockDir          string
}

func DefaultConfig() Config {
	return Config{
		WorkingPercent:   20,
		ProgressInterval: 5 * time.Second,
		MinChunk:         8 * human.MiB,
		MaxChunk:         512 * human.MiB,
		SampleSize:       32 * human.MiB,
		StateDir:         "/var/lib/logcut",
		LockDir:          "/var/lock",
	}
}

func Run(cfg Config) error {
	startedAt := time.Now()
	vlogf(cfg, "starting source=%q output=%q gzip=%t dry_run=%t", cfg.Source, cfg.Output, cfg.GzipOutput, cfg.DryRun)

	absSrc, _ := filepath.Abs(cfg.Source)
	absOut, _ := filepath.Abs(cfg.Output)
	cfg.Source = absSrc
	cfg.Output = absOut

	if cfg.Source == cfg.Output {
		return errors.New("source and output cannot be the same file")
	}

	vlogf(cfg, "scanning source log")
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

	stateID := shaForPath(cfg.Source + "|" + cfg.Output)
	statePath := filepath.Join(cfg.StateDir, stateID+".state")
	lockPath := filepath.Join(cfg.LockDir, "logcut-"+stateID+".lock")

	vlogf(cfg, "acquiring lock path=%q", lockPath)
	lk, err := lock.Acquire(lockPath)
	if err != nil {
		return err
	}
	defer lk.Close()

	outDir := filepath.Dir(cfg.Output)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	vlogf(cfg, "checking punch-hole support")
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

	vlogf(cfg, "sampling compression ratio sample_size=%s", human.FormatBytes(cfg.SampleSize))
	ratio, err := estimateCompressionRatio(src, 0, cfg.SampleSize, cfg.GzipOutput)
	if err != nil {
		return fmt.Errorf("compression sample failed: %w", err)
	}

	if !cfg.GzipOutput && !cfg.Force && free < 2*human.GiB {
		return errors.New("plain mode on low disk is risky; use -g for gzip output or pass --force")
	}

	initialChunk := chooseChunkSize(free, cfg.WorkingPercent, ratio, cfg.MinChunk, cfg.MaxChunk)
	printPlan(cfg, info, cutoff, free, ratio, initialChunk, statePath)

	if initialChunk <= 0 {
		return errors.New("not enough free space even with minimum chunk")
	}
	if cfg.DryRun {
		infof(cfg, "dry-run complete; no files changed")
		return nil
	}

	jobState := &state.State{Source: cfg.Source, Output: cfg.Output, Inode: info.Inode, Device: info.Device, OriginalSize: info.Size, CutoffOffset: cutoff, Gzip: cfg.GzipOutput}
	if old, err := state.Load(statePath); err == nil {
		if old.Source == cfg.Source && old.Output == cfg.Output && old.Inode == info.Inode && old.Device == info.Device && old.CutoffOffset == cutoff && old.Gzip == cfg.GzipOutput {
			jobState = old
			infof(cfg, "resuming from state offset=%s", human.FormatBytes(jobState.LastPunchedOffset))
		} else {
			return errors.New("existing state file does not match this job; remove it manually if you are sure")
		}
	}

	offset := jobState.LastPunchedOffset
	if offset < 0 || offset > cutoff {
		return errors.New("invalid state offset")
	}

	reporter := progress.New(os.Stdout, cutoff, offset, cfg.ProgressInterval, cfg.Quiet, cfg.Verbose)
	reporter.Start()

	chunkNo := 0
	lastRatio := ratio
	for offset < cutoff {
		chunkNo++
		freeBefore, err := disk.FreeBytes(outDir)
		if err != nil {
			return err
		}
		chunkSize := chooseChunkSize(freeBefore, cfg.WorkingPercent, lastRatio, cfg.MinChunk, cfg.MaxChunk)
		if chunkSize <= 0 {
			return fmt.Errorf("free space too low for next safe chunk; current free=%s", human.FormatBytes(freeBefore))
		}
		if offset+chunkSize > cutoff {
			chunkSize = cutoff - offset
		}

		chunkStarted := time.Now()
		vlogf(cfg, "chunk=%d status=read offset=%s target_chunk=%s", chunkNo, human.FormatBytes(offset), human.FormatBytes(chunkSize))
		data, end, err := readLineSafeChunk(src, offset, chunkSize, cutoff)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if len(data) == 0 || end <= offset {
			break
		}

		outBefore := int64(0)
		if st, err := os.Stat(cfg.Output); err == nil {
			outBefore = st.Size()
		}

		vlogf(cfg, "chunk=%d status=archive raw=%s", chunkNo, human.FormatBytes(int64(len(data))))
		if err := appendData(cfg.Output, data, cfg.GzipOutput); err != nil {
			return fmt.Errorf("archive append failed at offset %d: %w", offset, err)
		}
		outAfter := int64(0)
		if st, err := os.Stat(cfg.Output); err == nil {
			outAfter = st.Size()
		}
		written := outAfter - outBefore
		if written <= 0 {
			return errors.New("archive append wrote zero bytes; refusing to punch")
		}

		jobState.LastArchivedOffset = end
		if err := state.Save(statePath, jobState); err != nil {
			return err
		}

		vlogf(cfg, "chunk=%d status=punch offset=%s length=%s", chunkNo, human.FormatBytes(offset), human.FormatBytes(end-offset))
		if err := disk.PunchHole(src, offset, end-offset); err != nil {
			return fmt.Errorf("punch-hole failed at offset %d length %d: %w", offset, end-offset, err)
		}
		if err := src.Sync(); err != nil {
			return err
		}

		jobState.LastPunchedOffset = end
		if err := state.Save(statePath, jobState); err != nil {
			return err
		}

		raw := int64(len(data))
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
		freeAfter, _ := disk.FreeBytes(outDir)
		nextChunkSize := chooseChunkSize(freeAfter, cfg.WorkingPercent, lastRatio, cfg.MinChunk, cfg.MaxChunk)
		reporter.Chunk(progress.Snapshot{Chunk: chunkNo, Offset: offset, RawBytes: raw, ArchivedBytes: written, FreeBefore: freeBefore, FreeAfter: freeAfter, NextChunkSize: nextChunkSize, Ratio: lastRatio, ChunkDuration: time.Since(chunkStarted)})
	}

	if cfg.GzipOutput {
		vlogf(cfg, "verifying gzip archive path=%q", cfg.Output)
		if err := verifyGzip(cfg.Output); err != nil {
			return fmt.Errorf("gzip verification failed: %w", err)
		}
	}

	finalInfo, _ := disk.FileInfo(cfg.Source)
	reporter.Complete(offset)
	fmt.Println("Complete.")
	fmt.Println("  Active log apparent size:", human.FormatBytes(finalInfo.Size))
	fmt.Println("  Active log real usage:   ", human.FormatBytes(finalInfo.BlocksUsed))
	if st, err := os.Stat(cfg.Output); err == nil {
		fmt.Println("  Rotated output size:     ", human.FormatBytes(st.Size()))
	}
	fmt.Println("  Total runtime:           ", time.Since(startedAt).Round(time.Second))
	fmt.Println("  Check real usage with: du -h", cfg.Source)
	return nil
}

func printPlan(cfg Config, info disk.FileStats, cutoff int64, free int64, ratio float64, initialChunk int64, statePath string) {
	if cfg.Quiet {
		return
	}
	fmt.Println("Plan:")
	fmt.Println("  Source:           ", cfg.Source)
	fmt.Println("  Output:           ", cfg.Output)
	fmt.Println("  Gzip output:      ", cfg.GzipOutput)
	fmt.Println("  Verbose:          ", cfg.Verbose)
	fmt.Println("  Log apparent size:", human.FormatBytes(info.Size))
	fmt.Println("  Log real usage:   ", human.FormatBytes(info.BlocksUsed))
	fmt.Println("  Keep latest:      ", human.FormatBytes(cfg.KeepLastBytes))
	fmt.Println("  Rotate old range: ", human.FormatBytes(cutoff))
	fmt.Println("  Free space:       ", human.FormatBytes(free))
	fmt.Println("  Work budget:      ", fmt.Sprintf("%d%% of free space", cfg.WorkingPercent))
	fmt.Println("  Compression ratio:", fmt.Sprintf("%.2f%%", ratio*100))
	fmt.Println("  Initial chunk:    ", human.FormatBytes(initialChunk))
	fmt.Println("  Progress interval:", cfg.ProgressInterval)
	fmt.Println("  State file:       ", statePath)
}

func infof(cfg Config, format string, args ...interface{}) {
	if cfg.Quiet {
		return
	}
	fmt.Printf("[%s] "+format+"\n", append([]interface{}{time.Now().Format("2006-01-02 15:04:05")}, args...)...)
}

func vlogf(cfg Config, format string, args ...interface{}) {
	if cfg.Quiet || !cfg.Verbose {
		return
	}
	fmt.Printf("[%s] verbose: "+format+"\n", append([]interface{}{time.Now().Format("2006-01-02 15:04:05")}, args...)...)
}

func shaForPath(p string) string {
	h := sha1.Sum([]byte(p))
	return hex.EncodeToString(h[:])[:16]
}

func estimateCompressionRatio(src *os.File, offset, max int64, gzipEnabled bool) (float64, error) {
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
	gw := gzip.NewWriter(pw)
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

func readLineSafeChunk(src *os.File, start, target, cutoff int64) ([]byte, int64, error) {
	if start >= cutoff {
		return nil, start, io.EOF
	}
	maxEnd := start + target
	if maxEnd > cutoff {
		maxEnd = cutoff
	}
	if _, err := src.Seek(start, io.SeekStart); err != nil {
		return nil, start, err
	}
	reader := bufio.NewReaderSize(src, 1024*1024)
	var out []byte
	pos := start
	for pos < cutoff {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if pos+int64(len(line)) > cutoff {
				break
			}
			out = append(out, line...)
			pos += int64(len(line))
			if pos >= maxEnd && strings.HasSuffix(string(line), "\n") {
				break
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, start, err
		}
		if int64(len(out)) > target+64*human.MiB {
			break
		}
	}
	if len(out) == 0 {
		return nil, start, io.EOF
	}
	return out, pos, nil
}

func appendData(outputPath string, data []byte, gzipEnabled bool) error {
	out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	if gzipEnabled {
		gw := gzip.NewWriter(out)
		if _, err := gw.Write(data); err != nil {
			_ = gw.Close()
			_ = out.Close()
			return err
		}
		if err := gw.Close(); err != nil {
			_ = out.Close()
			return err
		}
	} else {
		if _, err := out.Write(data); err != nil {
			_ = out.Close()
			return err
		}
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(outputPath)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
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
