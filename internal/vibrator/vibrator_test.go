package vibrator

import (
	"testing"
	"time"

	"periph.io/x/conn/v3/gpio"
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

// stubPin records pin state changes with timestamps to measure actual duty cycle.
type stubPin struct {
	states []struct {
		level gpio.Level
		at    time.Time
	}
}

func (p *stubPin) Out(l gpio.Level) error {
	p.states = append(p.states, struct {
		level gpio.Level
		at    time.Time
	}{l, time.Now()})
	return nil
}

func (p *stubPin) String() string   { return "stub" }
func (p *stubPin) Halt() error      { return nil }
func (p *stubPin) Name() string     { return "stub" }
func (p *stubPin) Number() int      { return -1 }
func (p *stubPin) Function() string { return "OUT" }

// TestSoftwarePWMDutyCycle verifies that softwarePWM drives the pin HIGH for
// approximately the expected fraction of time based on the requested intensity.
func TestSoftwarePWMDutyCycle(t *testing.T) {
	tests := []struct {
		name      string
		intensity float64
	}{
		{"25%", 0.25},
		{"50%", 0.50},
		{"75%", 0.75},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pin := &stubPin{}
			softwarePWM(pin, tt.intensity, 100*time.Millisecond)

			if len(pin.states) < 2 {
				t.Fatal("not enough pin transitions recorded")
			}

			// Measure total time spent HIGH by summing durations between consecutive transitions.
			var highDur, totalDur time.Duration
			for i := 0; i < len(pin.states)-1; i++ {
				seg := pin.states[i+1].at.Sub(pin.states[i].at)
				totalDur += seg
				if pin.states[i].level == gpio.High {
					highDur += seg
				}
			}
			if totalDur == 0 {
				t.Fatal("zero total duration measured")
			}

			got := float64(highDur) / float64(totalDur)
			want := tt.intensity
			// Allow ±15% tolerance due to scheduling jitter
			if got < want-0.15 || got > want+0.15 {
				t.Errorf("duty cycle: got %.2f, want ~%.2f (highDur=%v, totalDur=%v)", got, want, highDur, totalDur)
			}
		})
	}
}

// TestSoftwarePWMEndsLow verifies the pin is driven LOW at the end of softwarePWM.
func TestSoftwarePWMEndsLow(t *testing.T) {
	pin := &stubPin{}
	softwarePWM(pin, 0.5, 50*time.Millisecond)
	if len(pin.states) == 0 {
		t.Fatal("no pin transitions recorded")
	}
	last := pin.states[len(pin.states)-1].level
	if last != gpio.Low {
		t.Errorf("expected pin to end LOW, got %v", last)
	}
}
