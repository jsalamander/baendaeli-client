package colorsensor

import (
	"io"
	"log"
	"testing"
	"time"

	"github.com/jsalamander/baendaeli-client/internal/config"
)

type stubBuzzer struct {
	count int
}

func (b *stubBuzzer) Buzz(_ float64, _ time.Duration) error {
	b.count++
	return nil
}

func silentLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

func TestWaitForBallDetectsMovementInSimMode(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	cfg := &config.Config{
		ColorSensorEnabled:           true,
		ColorSensorMovementThreshold: 1,
		ColorSensorCheckDurationMs:   400,
		ColorSensorVibrateBursts:     0,
		ColorSensorMaxAttempts:       1,
	}

	err := WaitForBall(s, nil, cfg, silentLogger(), nil)
	if err != nil {
		t.Fatalf("expected movement detection, got error: %v", err)
	}
}

func TestWaitForBallReturnsErrNoBallDetectedAfterMaxAttempts(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	b := &stubBuzzer{}
	cfg := &config.Config{
		ColorSensorEnabled:           true,
		ColorSensorMovementThreshold: 10000,
		ColorSensorCheckDurationMs:   1,
		ColorSensorVibrateIntensity:  0.8,
		ColorSensorVibrateDurationMs: 1,
		ColorSensorVibrateBursts:     1,
		ColorSensorMaxAttempts:       1,
	}

	err := WaitForBall(s, b, cfg, silentLogger(), nil)
	if err == nil {
		t.Fatal("expected ErrNoBallDetected, got nil")
	}
	if err != ErrNoBallDetected {
		t.Fatalf("expected ErrNoBallDetected, got %v", err)
	}
	if b.count != 1 {
		t.Fatalf("expected 1 vibration burst, got %d", b.count)
	}
}

func TestWaitForBallCallsAttemptObserver(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	cfg := &config.Config{
		ColorSensorEnabled:           true,
		ColorSensorMovementThreshold: 10000,
		ColorSensorCheckDurationMs:   1,
		ColorSensorVibrateBursts:     0,
		ColorSensorMaxAttempts:       3,
	}

	var got []int
	err := WaitForBall(s, nil, cfg, silentLogger(), func(attempt int, _ int) {
		got = append(got, attempt)
	})
	if err == nil {
		t.Fatal("expected ErrNoBallDetected, got nil")
	}
	if len(got) != 3 {
		t.Fatalf("expected observer to be called 3 times, got %d", len(got))
	}
	for i := 1; i <= 3; i++ {
		if got[i-1] != i {
			t.Fatalf("expected attempt %d at index %d, got %d", i, i-1, got[i-1])
		}
	}
}
