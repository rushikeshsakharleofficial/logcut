package compact

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rushikeshsakharleofficial/logcut/internal/human"
)

func TestChooseChunkSize(t *testing.T) {
	got := chooseChunkSize(10*human.GiB, 20, 0.5, 8*human.MiB, 512*human.MiB)
	if got <= 0 {
		t.Fatalf("expected positive chunk size")
	}
	if got > 512*human.MiB {
		t.Fatalf("chunk size exceeded max: %d", got)
	}
}

func TestChooseChunkSizeTooLow(t *testing.T) {
	got := chooseChunkSize(1*human.MiB, 20, 1.0, 8*human.MiB, 512*human.MiB)
	if got != 0 {
		t.Fatalf("expected zero chunk for low free space, got %d", got)
	}
}

func TestApplyPacingNoPanic(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RateLimitBytes = 100 * human.MiB
	cfg.SleepBetweenChunks = 1 * time.Millisecond
	applyPacing(cfg, 1*human.MiB, 1*time.Millisecond)
}

func TestAppendLineSafeChunkPlain(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.log")
	outputPath := filepath.Join(dir, "archive.log")
	sourceData := []byte("a\nbb\nccc\n")
	if err := os.WriteFile(sourcePath, sourceData, 0644); err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	end, raw, written, err := appendLineSafeChunk(outputPath, src, 0, 3, int64(len(sourceData)), false, gzip.NoCompression)
	if err != nil {
		t.Fatal(err)
	}
	if end != 5 || raw != 5 || written != 5 {
		t.Fatalf("end=%d raw=%d written=%d, want 5/5/5", end, raw, written)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a\nbb\n" {
		t.Fatalf("output=%q", string(got))
	}
}

func TestAppendLineSafeChunkGzip(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.log")
	outputPath := filepath.Join(dir, "archive.log.gz")
	sourceData := []byte("alpha\nbeta\n")
	if err := os.WriteFile(sourcePath, sourceData, 0644); err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	end, raw, written, err := appendLineSafeChunk(outputPath, src, 0, int64(len(sourceData)), int64(len(sourceData)), true, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	if end != int64(len(sourceData)) || raw != int64(len(sourceData)) || written <= 0 {
		t.Fatalf("end=%d raw=%d written=%d", end, raw, written)
	}
	out, err := os.Open(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	gr, err := gzip.NewReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(sourceData) {
		t.Fatalf("gzip output=%q", string(got))
	}
}
