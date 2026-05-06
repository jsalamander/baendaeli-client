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

	err := WaitForBall(s, nil, cfg, silentLogger())
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

	err := WaitForBall(s, b, cfg, silentLogger())
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
