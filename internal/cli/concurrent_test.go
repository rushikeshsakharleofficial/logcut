package cli

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rushikeshsakharleofficial/logcut/internal/disk"
)

// startConcurrentWriter opens path O_APPEND and writes timestamped log lines at
// bytesPerSec rate (0 = unbounded). Returns stop() which halts the writer and
// returns all bytes it wrote. t.Cleanup ensures the goroutine stops even on
// test failure.
func startConcurrentWriter(t *testing.T, path string, bytesPerSec int64) func() []byte {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("startConcurrentWriter open %s: %v", path, err)
	}
	id := fmt.Sprintf("w%d", time.Now().UnixNano())
	var mu sync.Mutex
	var written []byte
	stop := make(chan struct{})
	var once sync.Once
	stopped := make(chan struct{})
	closeStop := func() { once.Do(func() { close(stop) }) }

	go func() {
		defer close(stopped)
		defer f.Close()
		seq := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			seq++
			start := time.Now()
			line := []byte(fmt.Sprintf("[%s] %s seq=%08d sentinel=logcut-concurrent-test\n",
				time.Now().Format("2006-01-02T15:04:05.000"), id, seq))
			if _, err := f.Write(line); err != nil {
				return
			}
			mu.Lock()
			written = append(written, line...)
			mu.Unlock()
			if bytesPerSec > 0 {
				elapsed := time.Since(start)
				budget := time.Duration(float64(len(line)) / float64(bytesPerSec) * float64(time.Second))
				if budget > elapsed {
					time.Sleep(budget - elapsed)
				}
			}
		}
	}()

	t.Cleanup(func() {
		closeStop()
		<-stopped
	})
	return func() []byte {
		closeStop()
		<-stopped
		mu.Lock()
		defer mu.Unlock()
		return append([]byte(nil), written...)
	}
}

// readGzipFileBytes decompresses a (possibly multi-member) gzip file into []byte.
func readGzipFileBytes(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var buf bytes.Buffer
	for {
		gr, err := gzip.NewReader(f)
		if err == io.EOF {
			break
		}
		if err != nil {
			if err.Error() == "unexpected EOF" {
				break
			}
			return nil, fmt.Errorf("gzip reader: %w", err)
		}
		if _, copyErr := io.Copy(&buf, gr); copyErr != nil {
			_ = gr.Close()
			return nil, fmt.Errorf("gzip copy: %w", copyErr)
		}
		_ = gr.Close()
	}
	return buf.Bytes(), nil
}

// writerID extracts the unique writer identifier from the first line of writer
// output (second space-delimited token: "w<timestamp>").
func writerID(data []byte) []byte {
	parts := bytes.SplitN(data, []byte(" "), 3)
	if len(parts) >= 2 {
		return parts[1]
	}
	return nil
}

// TestIntegrationConcurrentWriterStability runs logcut while a goroutine
// appends 1 MB/s to the source. Verifies: exit 0, valid gzip, sparse source,
// writer data intact in source tail.
func TestIntegrationConcurrentWriterStability(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 500k lines × 1 KB = ~500 MB initial log
	if err := generateLog(source, 500_000, 1024); err != nil {
		t.Fatal(err)
	}
	info, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("source: %s", humanize(info.Size))

	stopWriter := startConcurrentWriter(t, source, 1*1024*1024) // 1 MB/s

	code := Run([]string{
		"--quiet", "-g", "--compress-level", "1", "--verify", "full",
		"-k", fmt.Sprintf("%d", info.Size/10),
		"--state-dir", stateDir, "--lock-dir", lockDir,
		source, output,
	})
	if code != 0 {
		t.Fatalf("Run returned %d", code)
	}

	writerData := stopWriter()

	if _, err := readGzipFileBytes(output); err != nil {
		t.Fatalf("output gzip invalid: %v", err)
	}
	outStat, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if outStat.Size() == 0 {
		t.Fatal("output gzip is empty")
	}
	t.Logf("output gzip: %s", humanize(outStat.Size()))

	finalInfo, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	if finalInfo.BlocksUsed >= finalInfo.Size {
		t.Errorf("source not sparse: apparent=%s blocks=%s",
			humanize(finalInfo.Size), humanize(finalInfo.BlocksUsed))
	}
	t.Logf("source sparse: apparent=%s real=%s",
		humanize(finalInfo.Size), humanize(finalInfo.BlocksUsed))

	if len(writerData) == 0 {
		t.Fatal("writer wrote nothing")
	}
	id := writerID(writerData)
	if id == nil {
		t.Fatal("cannot parse writer id")
	}
	sf, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()
	if _, err := sf.Seek(-4*1024*1024, io.SeekEnd); err != nil {
		// file shorter than 4 MB — seek to start
		if _, err2 := sf.Seek(0, io.SeekStart); err2 != nil {
			t.Fatal(err2)
		}
	}
	tail, err := io.ReadAll(sf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(tail, id) {
		t.Errorf("writer sentinel %q not found in source tail", id)
	}
	t.Logf("writer wrote %s, sentinel found", humanize(int64(len(writerData))))
}

// TestIntegrationConcurrentWriterIntegrity verifies byte-exact correctness:
// the archived portion must equal origData[0:cutoff] and the source keep region
// must equal origData[cutoff:]. Writer appends only beyond EOF so ranges never
// overlap.
func TestIntegrationConcurrentWriterIntegrity(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := generateLog(source, 500_000, 1024); err != nil {
		t.Fatal(err)
	}
	info, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	logSize := info.Size
	keepBytes := logSize / 10
	cutoff := logSize - keepBytes
	t.Logf("source=%s cutoff=%s keep=%s", humanize(logSize), humanize(cutoff), humanize(keepBytes))

	// Snapshot original data before any modification (holes become zeros after punch)
	origData, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}

	stopWriter := startConcurrentWriter(t, source, 256*1024) // 256 KB/s

	code := Run([]string{
		"--quiet", "-g", "--compress-level", "1", "--verify", "full",
		"-k", fmt.Sprintf("%d", keepBytes),
		"--state-dir", stateDir, "--lock-dir", lockDir,
		source, output,
	})
	if code != 0 {
		t.Fatalf("Run returned %d", code)
	}

	writerData := stopWriter()
	t.Logf("writer appended %s", humanize(int64(len(writerData))))

	// Archive must decompress to exactly origData[0:cutoff]
	archived, err := readGzipFileBytes(output)
	if err != nil {
		t.Fatalf("read gzip: %v", err)
	}
	if int64(len(archived)) != cutoff {
		t.Fatalf("archive decompressed %s, want %s (cutoff)", humanize(int64(len(archived))), humanize(cutoff))
	}
	if !bytes.Equal(archived, origData[:cutoff]) {
		for i := range archived {
			if archived[i] != origData[i] {
				t.Fatalf("archive mismatch at byte %d: got %02x want %02x", i, archived[i], origData[i])
			}
		}
	}
	t.Logf("archive byte-exact match: %s", humanize(int64(len(archived))))

	// Source [cutoff:logSize] must equal origData[cutoff:]
	sf, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()
	if _, err := sf.Seek(cutoff, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	keepRegion := make([]byte, logSize-cutoff)
	if _, err := io.ReadFull(sf, keepRegion); err != nil {
		t.Fatalf("read source keep region: %v", err)
	}
	if !bytes.Equal(keepRegion, origData[cutoff:]) {
		t.Fatalf("source keep region mismatch: len got=%d want=%d", len(keepRegion), len(origData[cutoff:]))
	}
	t.Logf("source keep region byte-exact match: %s", humanize(int64(len(keepRegion))))

	// Writer bytes must follow immediately after the original tail
	if len(writerData) > 0 {
		writerRegion := make([]byte, len(writerData))
		if _, err := io.ReadFull(sf, writerRegion); err != nil {
			t.Fatalf("read writer region: %v", err)
		}
		if !bytes.Equal(writerRegion, writerData) {
			t.Fatalf("writer region mismatch: %d bytes", len(writerData))
		}
		t.Logf("writer data byte-exact match: %s", humanize(int64(len(writerData))))
	}
}

// TestIntegrationConcurrentWriterBurst runs three concurrent writers —
// trickle, and two unbounded — while logcut archives with a rate limit.
// Proves no deadlock, fd conflict, or corruption under sustained concurrent IO.
func TestIntegrationConcurrentWriterBurst(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.log")
	output := filepath.Join(dir, "app.rotated.log.gz")
	stateDir := filepath.Join(dir, "state")
	lockDir := filepath.Join(dir, "lock")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := generateLog(source, 500_000, 1024); err != nil {
		t.Fatal(err)
	}
	info, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	origLogSize := info.Size
	t.Logf("source: %s", humanize(origLogSize))

	stopTrickle := startConcurrentWriter(t, source, 256*1024) // 256 KB/s
	stopBurst1 := startConcurrentWriter(t, source, 0)         // unbounded
	stopBurst2 := startConcurrentWriter(t, source, 0)         // unbounded

	// Rate-limit logcut so writers generate meaningful data before it finishes
	code := Run([]string{
		"--quiet", "-g", "--compress-level", "1", "--verify", "full",
		"-k", fmt.Sprintf("%d", origLogSize/10),
		"--rate-limit", "50M",
		"--state-dir", stateDir, "--lock-dir", lockDir,
		source, output,
	})
	if code != 0 {
		t.Fatalf("Run returned %d", code)
	}

	trickleData := stopTrickle()
	burst1Data := stopBurst1()
	burst2Data := stopBurst2()
	t.Logf("writers wrote: trickle=%s burst1=%s burst2=%s",
		humanize(int64(len(trickleData))),
		humanize(int64(len(burst1Data))),
		humanize(int64(len(burst2Data))))

	if _, err := readGzipFileBytes(output); err != nil {
		t.Fatalf("output gzip invalid: %v", err)
	}

	finalInfo, err := disk.FileInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	if finalInfo.BlocksUsed >= finalInfo.Size {
		t.Errorf("source not sparse: apparent=%s blocks=%s",
			humanize(finalInfo.Size), humanize(finalInfo.BlocksUsed))
	}

	// Read the full appended region (from original EOF onwards) to find all
	// writers' sentinels — burst writers may append hundreds of MB, so the
	// trickle's data is NOT in the last N bytes; it's somewhere in the middle.
	sf, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()
	if _, err := sf.Seek(origLogSize, io.SeekStart); err != nil {
		t.Fatalf("seek to append zone: %v", err)
	}
	appendedData, err := io.ReadAll(sf)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("appended zone: %s", humanize(int64(len(appendedData))))

	checkSentinel := func(label string, data []byte) {
		if len(data) == 0 {
			t.Logf("%s: wrote nothing (too fast for rate limit)", label)
			return
		}
		id := writerID(data)
		if id == nil {
			t.Errorf("%s: cannot parse writer id", label)
			return
		}
		if bytes.Contains(appendedData, id) {
			t.Logf("%s: sentinel found (%s written)", label, humanize(int64(len(data))))
		} else {
			t.Errorf("%s: sentinel %q not in appended zone", label, id)
		}
	}
	checkSentinel("trickle", trickleData)
	checkSentinel("burst1", burst1Data)
	checkSentinel("burst2", burst2Data)
}
