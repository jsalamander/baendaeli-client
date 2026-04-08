package vibrator

import (
	"testing"
	"time"
)

// ensure Init is a no-op when disabled and does not set the global vib
func TestInitDisabledLeavesVibNil(t *testing.T) {
	prev := vib
	vib = nil
	defer func() { vib = prev }()

	if err := Init(Config{Enabled: false}); err != nil {
		t.Fatalf("Init returned error for disabled config: %v", err)
	}
	if vib != nil {
		t.Fatalf("vib should remain nil when disabled")
	}
}

// ensure Buzz with nil vib is a safe no-op
func TestBuzzNilSafe(t *testing.T) {
	prev := vib
	vib = nil
	defer func() { vib = prev }()

	if err := Buzz(0.5, 10*time.Millisecond); err != nil {
		t.Fatalf("Buzz with nil vib returned error: %v", err)
	}
}

// ensure Cleanup tolerates nil vib without panicking
func TestCleanupNilSafe(t *testing.T) {
	prev := vib
	vib = nil
	defer func() { vib = prev }()

	Cleanup() // must not panic
}

// validate Buzz sleeps approximately the given duration in simulation mode
func TestBuzzSimTiming(t *testing.T) {
	prev := vib
	vib = &vibrator{sim: true}
	defer func() { vib = prev }()

	buzz := 50 * time.Millisecond
	start := time.Now()
	if err := Buzz(0.5, buzz); err != nil {
		t.Fatalf("Buzz returned error: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < buzz || elapsed > buzz+100*time.Millisecond {
		t.Fatalf("unexpected elapsed time: %v (want ~%v)", elapsed, buzz)
	}
}
