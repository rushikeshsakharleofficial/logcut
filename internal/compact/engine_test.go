package compact

import (
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
