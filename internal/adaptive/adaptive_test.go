package adaptive

import (
	"testing"
	"time"

	"github.com/rushikeshsakharleofficial/logcut/internal/human"
)

func TestEvaluateSmoke(t *testing.T) {
	snap := Evaluate(".")
	if snap.Pressure == "" {
		t.Fatal("Evaluate returned empty Pressure")
	}
	if snap.RateLimitBytes <= 0 {
		t.Fatal("Evaluate returned non-positive RateLimitBytes")
	}
}

// Small-memory machine under pressure (40% available): expect high throttle.
func TestClassifySmallMemoryMachineUnderPressure(t *testing.T) {
	s := classify(8, 0.2, 0.025, 2*human.GiB, 5*human.GiB, 0.40, 100*human.GiB)
	if s.Pressure != "high" {
		t.Fatalf("Pressure=%q, want high", s.Pressure)
	}
	if s.RateLimitBytes != 25*human.MiB {
		t.Fatalf("RateLimitBytes=%d, want %d", s.RateLimitBytes, 25*human.MiB)
	}
	if s.SleepBetweenChunks != 2*time.Second {
		t.Fatalf("SleepBetweenChunks=%s, want 2s", s.SleepBetweenChunks)
	}
	if s.MaxChunkBytes != 64*human.MiB {
		t.Fatalf("MaxChunkBytes=%d, want %d", s.MaxChunkBytes, 64*human.MiB)
	}
}

// Small-memory machine with ample free memory: should NOT throttle more than normal.
func TestClassifySmallMemoryMachineIdle(t *testing.T) {
	s := classify(8, 0.2, 0.025, 4*human.GiB, 5*human.GiB, 0.80, 100*human.GiB)
	if s.Pressure != "low" {
		t.Fatalf("idle small-memory machine: Pressure=%q, want low", s.Pressure)
	}
}

// Constrained-memory machine under moderate pressure (50% available): expect medium throttle.
func TestClassifyConstrainedMemoryMachineUnderPressure(t *testing.T) {
	s := classify(8, 0.2, 0.025, 4*human.GiB, 8*human.GiB, 0.50, 100*human.GiB)
	if s.Pressure != "medium" {
		t.Fatalf("Pressure=%q, want medium", s.Pressure)
	}
	if s.RateLimitBytes != 50*human.MiB {
		t.Fatalf("RateLimitBytes=%d, want %d", s.RateLimitBytes, 50*human.MiB)
	}
	if s.MaxChunkBytes != 128*human.MiB {
		t.Fatalf("MaxChunkBytes=%d, want %d", s.MaxChunkBytes, 128*human.MiB)
	}
}
