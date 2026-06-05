package main

import (
	"bufio"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	KiB int64 = 1024
	MiB int64 = 1024 * KiB
	GiB int64 = 1024 * MiB

	FALLOC_FL_KEEP_SIZE  = 0x01
	FALLOC_FL_PUNCH_HOLE = 0x02
)

type Config struct {
	gzipOutput     bool
	keepLastRaw    string
	workingPercent int64
	dryRun         bool
	force          bool
	source         string
	output         string
	keepLastBytes  int64
	minChunk       int64
	maxChunk       int64
	sampleSize     int64
	statePath      string
	lockPath       string
}

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

func usage() {
	fmt.Println("logcut - emergency log compaction without app restart")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  logcut [options] <source-log> <rotated-output>")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  logcut app.log app.rotated.log")
	fmt.Println("  logcut -g app.log app.rotated.log.gz")
	fmt.Println("  logcut -g -k 10G app.log app.rotated.log.gz")
	fmt.Println("  logcut --dry-run -g -k 10G app.log app.rotated.log.gz")
	fmt.Println("")
	fmt.Println("Options:")
	fmt.Println("  -g              write gzip rotated archive")
	fmt.Println("  -k <size>       keep latest part in active log, default: 10% of source size")
	fmt.Println("  -p <percent>    use only this % of current free space as working budget, default: 20")
	fmt.Println("  --dry-run       print plan only, do not modify files")
	fmt.Println("  --force         allow risky plain output on low disk")
}

func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, errors.New("empty size")
	}

	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "GB"):
		mult = GiB
		s = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "G"):
		mult = GiB
		s = strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "MB"):
		mult = MiB
		s = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "M"):
		mult = MiB
		s = strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "KB"):
		mult = KiB
		s = strings.TrimSuffix(s, "KB")
	case strings.HasSuffix(s, "K"):
		mult = KiB
		s = strings.TrimSuffix(s, "K")
	}

	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, errors.New("size must be greater than zero")
	}
	return int64(n * float64(mult)), nil
}

func formatBytes(n int64) string {
	if n >= GiB {
		return fmt.Sprintf("%.2fG", float64(n)/float64(GiB))
	}
	if n >= MiB {
		return fmt.Sprintf("%.2fM", float64(n)/float64(MiB))
	}
	if n >= KiB {
		return fmt.Sprintf("%.2fK", float64(n)/float64(KiB))
	}
	return fmt.Sprintf("%dB", n)
}

func fileInfo(path string) (size int64, blocksBytes int64, inode uint64, dev uint64, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return st.Size(), st.Size(), 0, 0, nil
	}
	return st.Size(), int64(sys.Blocks) * 512, sys.Ino, uint64(sys.Dev), nil
}

func freeBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	target := path
	if _, err := os.Stat(target); err != nil {
		target = filepath.Dir(path)
	}
	if err := syscall.Statfs(target, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func punchHole(f *os.File, offset int64, length int64) error {
	if length <= 0 {
		return nil
	}
	_, _, errno := syscall.Syscall6(
		syscall.SYS_FALLOCATE,
		uintptr(f.Fd()),
		uintptr(FALLOC_FL_KEEP_SIZE|FALLOC_FL_PUNCH_HOLE),
		uintptr(offset),
		uintptr(length),
		0,
		0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func testPunchHole(dir string) error {
	p := filepath.Join(dir, ".logcut-punch-test")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(p)
	defer f.Close()

	if err := f.Truncate(2 * MiB); err != nil {
		return err
	}
	if err := punchHole(f, 0, MiB); err != nil {
		return err
	}
	return nil
}

func shaForPath(p string) string {
	h := sha1.Sum([]byte(p))
	return hex.EncodeToString(h[:])[:16]
}

func loadState(path string) (*State, error) {
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
		k := parts[0]
		v := parts[1]
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

func saveState(path string, s *State) error {
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

func acquireLock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another logcut process may be running for this file: %w", err)
	}
	return f, nil
}

func estimateCompressionRatio(src *os.File, offset int64, max int64, gzipEnabled bool) (float64, error) {
	if !gzipEnabled {
		return 1.0, nil
	}
	if max <= 0 {
		max = 16 * MiB
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

func chooseChunkSize(free int64, percent int64, ratio float64, minChunk int64, maxChunk int64) int64 {
	if percent <= 0 || percent > 80 {
		percent = 20
	}
	workingBudget := free * percent / 100
	safeOutputLimit := workingBudget * 70 / 100
	if safeOutputLimit < 8*MiB {
		return 0
	}
	raw := int64(float64(safeOutputLimit) / ratio)
	if raw < minChunk {
		raw = minChunk
	}
	if raw > maxChunk {
		raw = maxChunk
	}
	// Round down to 1 MiB.
	raw = raw / MiB * MiB
	if raw < minChunk {
		raw = minChunk
	}
	return raw
}

func readLineSafeChunk(src *os.File, start int64, target int64, cutoff int64) ([]byte, int64, error) {
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
	var pos = start

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
		// Protect memory if a single line is huge.
		if int64(len(out)) > target+64*MiB {
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
			gw.Close()
			out.Close()
			return err
		}
		if err := gw.Close(); err != nil {
			out.Close()
			return err
		}
	} else {
		if _, err := out.Write(data); err != nil {
			out.Close()
			return err
		}
	}
	if err := out.Sync(); err != nil {
		out.Close()
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

func compact(cfg Config) error {
	absSrc, _ := filepath.Abs(cfg.source)
	absOut, _ := filepath.Abs(cfg.output)
	cfg.source = absSrc
	cfg.output = absOut

	if cfg.source == cfg.output {
		return errors.New("source and output cannot be the same file")
	}
	if strings.HasPrefix(cfg.output, cfg.source) && !cfg.force {
		// Avoid accidental same-ish naming is ok; this just guards weird exact prefix paths.
	}

	size, realUsage, inode, dev, err := fileInfo(cfg.source)
	if err != nil {
		return err
	}
	if size <= 0 {
		return errors.New("source log is empty")
	}

	if cfg.keepLastRaw != "" {
		cfg.keepLastBytes, err = parseSize(cfg.keepLastRaw)
		if err != nil {
			return fmt.Errorf("invalid keep-last value: %w", err)
		}
	} else {
		cfg.keepLastBytes = size / 10 // default: keep latest 10%.
		if cfg.keepLastBytes < 64*MiB && size > 640*MiB {
			cfg.keepLastBytes = 64 * MiB
		}
	}
	if cfg.keepLastBytes >= size {
		return fmt.Errorf("keep-last %s is greater/equal to log size %s", formatBytes(cfg.keepLastBytes), formatBytes(size))
	}

	cutoff := size - cfg.keepLastBytes
	if cutoff <= 0 {
		return errors.New("nothing to compact")
	}

	stateID := shaForPath(cfg.source + "|" + cfg.output)
	cfg.statePath = filepath.Join("/var/lib/logcut", stateID+".state")
	cfg.lockPath = filepath.Join("/var/lock", "logcut-"+stateID+".lock")

	lock, err := acquireLock(cfg.lockPath)
	if err != nil {
		return err
	}
	defer lock.Close()

	outDir := filepath.Dir(cfg.output)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	if err := testPunchHole(filepath.Dir(cfg.source)); err != nil {
		return fmt.Errorf("filesystem does not support punch-hole on source path or permission denied: %w", err)
	}

	src, err := os.OpenFile(cfg.source, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer src.Close()

	free, err := freeBytes(outDir)
	if err != nil {
		return err
	}

	ratio, err := estimateCompressionRatio(src, 0, cfg.sampleSize, cfg.gzipOutput)
	if err != nil {
		return fmt.Errorf("compression sample failed: %w", err)
	}

	if !cfg.gzipOutput && !cfg.force {
		// Plain mode needs roughly raw output space over time before hole punching per chunk.
		// It can still work chunk-by-chunk, but gives no net gain until punch completes and has higher risk.
		if free < 2*GiB {
			return errors.New("plain mode on low disk is risky; use -g for gzip output or pass --force")
		}
	}

	initialChunk := chooseChunkSize(free, cfg.workingPercent, ratio, cfg.minChunk, cfg.maxChunk)

	fmt.Println("Plan:")
	fmt.Println("  Source:           ", cfg.source)
	fmt.Println("  Output:           ", cfg.output)
	fmt.Println("  Gzip output:      ", cfg.gzipOutput)
	fmt.Println("  Log apparent size:", formatBytes(size))
	fmt.Println("  Log real usage:   ", formatBytes(realUsage))
	fmt.Println("  Keep latest:      ", formatBytes(cfg.keepLastBytes))
	fmt.Println("  Rotate old range: ", formatBytes(cutoff))
	fmt.Println("  Free space:       ", formatBytes(free))
	fmt.Println("  Work budget:      ", fmt.Sprintf("%d%% of free space", cfg.workingPercent))
	fmt.Println("  Compression ratio:", fmt.Sprintf("%.2f%%", ratio*100))
	fmt.Println("  Initial chunk:    ", formatBytes(initialChunk))
	fmt.Println("  State file:       ", cfg.statePath)

	if initialChunk <= 0 {
		return errors.New("not enough free space even with minimum chunk; free a little space or use a smaller environment-specific rescue action")
	}
	if cfg.dryRun {
		fmt.Println("Dry-run mode: no files changed.")
		return nil
	}

	state := &State{
		Source: cfg.source, Output: cfg.output, Inode: inode, Device: dev,
		OriginalSize: size, CutoffOffset: cutoff, Gzip: cfg.gzipOutput,
	}
	if old, err := loadState(cfg.statePath); err == nil {
		if old.Source == cfg.source && old.Output == cfg.output && old.Inode == inode && old.Device == dev && old.CutoffOffset == cutoff && old.Gzip == cfg.gzipOutput {
			state = old
			fmt.Println("Resuming from state offset:", formatBytes(state.LastPunchedOffset))
		} else {
			return errors.New("existing state file does not match this job; remove it manually if you are sure")
		}
	}

	offset := state.LastPunchedOffset
	if offset < 0 || offset > cutoff {
		return errors.New("invalid state offset")
	}

	chunkNo := 0
	lastRatio := ratio
	for offset < cutoff {
		chunkNo++
		freeNow, err := freeBytes(outDir)
		if err != nil {
			return err
		}
		chunkSize := chooseChunkSize(freeNow, cfg.workingPercent, lastRatio, cfg.minChunk, cfg.maxChunk)
		if chunkSize <= 0 {
			return fmt.Errorf("free space too low for next safe chunk; current free=%s", formatBytes(freeNow))
		}
		if offset+chunkSize > cutoff {
			chunkSize = cutoff - offset
		}

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
		if st, err := os.Stat(cfg.output); err == nil {
			outBefore = st.Size()
		}

		started := time.Now()
		if err := appendData(cfg.output, data, cfg.gzipOutput); err != nil {
			return fmt.Errorf("archive append failed at offset %d: %w", offset, err)
		}
		outAfter := int64(0)
		if st, err := os.Stat(cfg.output); err == nil {
			outAfter = st.Size()
		}
		written := outAfter - outBefore
		if written <= 0 {
			return errors.New("archive append wrote zero bytes; refusing to punch")
		}

		state.LastArchivedOffset = end
		if err := saveState(cfg.statePath, state); err != nil {
			return err
		}

		if err := punchHole(src, offset, end-offset); err != nil {
			return fmt.Errorf("punch-hole failed at offset %d length %d: %w", offset, end-offset)
		}
		if err := src.Sync(); err != nil {
			return err
		}

		state.LastPunchedOffset = end
		if err := saveState(cfg.statePath, state); err != nil {
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
		freeAfter, _ := freeBytes(outDir)
		fmt.Printf("chunk=%d raw=%s archived=%s punched=%s next_offset=%s free=%s ratio=%.2f%% time=%s\n",
			chunkNo, formatBytes(raw), formatBytes(written), formatBytes(raw), formatBytes(offset), formatBytes(freeAfter), lastRatio*100, time.Since(started).Round(time.Millisecond))
	}

	if cfg.gzipOutput {
		fmt.Println("Verifying gzip archive...")
		if err := verifyGzip(cfg.output); err != nil {
			return fmt.Errorf("gzip verification failed: %w", err)
		}
	}

	finalSize, finalReal, _, _, _ := fileInfo(cfg.source)
	fmt.Println("Complete.")
	fmt.Println("  Active log apparent size:", formatBytes(finalSize))
	fmt.Println("  Active log real usage:   ", formatBytes(finalReal))
	if st, err := os.Stat(cfg.output); err == nil {
		fmt.Println("  Rotated output size:     ", formatBytes(st.Size()))
	}
	fmt.Println("  Check real usage with: du -h", cfg.source)
	return nil
}

func main() {
	cfg := Config{}
	flag.BoolVar(&cfg.gzipOutput, "g", false, "write gzip rotated archive")
	flag.StringVar(&cfg.keepLastRaw, "k", "", "keep latest part in active log, example: 10G")
	flag.Int64Var(&cfg.workingPercent, "p", 20, "use only this percent of current free space as working budget")
	flag.BoolVar(&cfg.dryRun, "dry-run", false, "print plan only")
	flag.BoolVar(&cfg.force, "force", false, "allow risky operation")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) != 2 {
		usage()
		os.Exit(2)
	}

	cfg.source = args[0]
	cfg.output = args[1]
	cfg.minChunk = 8 * MiB
	cfg.maxChunk = 512 * MiB
	cfg.sampleSize = 32 * MiB

	if cfg.workingPercent <= 0 || cfg.workingPercent > 80 {
		fmt.Println("Invalid -p value. Use 1 to 80. Recommended: 20")
		os.Exit(2)
	}

	if err := compact(cfg); err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}
}
