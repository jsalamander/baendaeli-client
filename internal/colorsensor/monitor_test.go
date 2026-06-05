package colorsensor

import (
	"bytes"
	"io"
	"log"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jsalamander/baendaeli-client/internal/config"
)

type stubBuzzer struct {
	count       int
	intensities []float64
}

func (b *stubBuzzer) Buzz(intensity float64, _ time.Duration) error {
	b.count++
	b.intensities = append(b.intensities, intensity)
	return nil
}

func silentLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

func bufferLogger() (*log.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return log.New(buf, "", 0), buf
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
		t.Fatalf("expected vibration on the failed attempt, got %d", b.count)
	}
}

func TestWaitForBallVibratesBetweenMovementOnlyRetries(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	b := &stubBuzzer{}
	cfg := &config.Config{
		ColorSensorEnabled:           true,
		ColorSensorMovementThreshold: 10000,
		ColorSensorCheckDurationMs:   1,
		ColorSensorVibrateIntensity:  0.8,
		ColorSensorVibrateDurationMs: 1,
		ColorSensorVibrateBursts:     1,
		ColorSensorMaxAttempts:       3,
	}

	err := WaitForBall(s, b, cfg, silentLogger(), nil)
	if err != ErrNoBallDetected {
		t.Fatalf("expected ErrNoBallDetected, got %v", err)
	}
	if b.count != 3 {
		t.Fatalf("expected 3 vibration bursts (one per failed attempt), got %d", b.count)
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

func TestWaitForBallWithReferenceBaselineDetectsSettledBall(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	logger := silentLogger()

	withoutRefCfg := &config.Config{
		ColorSensorEnabled:           true,
		ColorSensorMovementThreshold: 100,
		ColorSensorPresenceTolerance: 2,
		ColorSensorPollIntervalMs:    1,
		ColorSensorCheckDurationMs:   5,
		ColorSensorStableSamples:     1,
		ColorSensorMaxAttempts:       1,
	}

	if err := WaitForBall(s, nil, withoutRefCfg, logger, nil); err == nil {
		t.Fatal("expected no detection without reference baseline")
	}

	// Reset simulation counter and retry with a ball-present reference baseline.
	s.simCount.Store(0)
	withRefCfg := &config.Config{
		ColorSensorEnabled:           true,
		ColorSensorMovementThreshold: 100,
		ColorSensorPresenceTolerance: 2,
		ColorSensorPollIntervalMs:    1,
		ColorSensorCheckDurationMs:   5,
		ColorSensorStableSamples:     1,
		ColorSensorMaxAttempts:       1,
	}
	reference := uint16(5)

	err := WaitForBallWithReferenceBaseline(s, nil, withRefCfg, logger, nil, reference)
	if err != nil {
		t.Fatalf("expected detection with reference baseline, got error: %v", err)
	}
}

func TestWaitForBallWithPresenceReferenceBaselineDetectsWithoutMovement(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	logger := silentLogger()

	cfg := &config.Config{
		ColorSensorEnabled:           true,
		ColorSensorMovementThreshold: 5,
		ColorSensorPresenceTolerance: 2,
		ColorSensorPollIntervalMs:    1,
		ColorSensorCheckDurationMs:   5,
		ColorSensorStableSamples:     1,
		ColorSensorMaxAttempts:       1,
	}

	// In simulation mode values are monotonic and start low. A nearby
	// reference should be matched immediately without requiring motion spikes.
	reference := uint16(6)

	err := WaitForBallWithPresenceReferenceBaseline(s, nil, cfg, logger, nil, reference)
	if err != nil {
		t.Fatalf("expected detection with presence reference baseline, got error: %v", err)
	}
}

func TestWaitForBallWithReferenceBaselineFallsBackToMovement(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	logger := silentLogger()

	cfg := &config.Config{
		ColorSensorEnabled:           true,
		ColorSensorMovementThreshold: 1,
		ColorSensorPresenceTolerance: 1,
		ColorSensorPollIntervalMs:    1,
		ColorSensorCheckDurationMs:   10,
		ColorSensorStableSamples:     1,
		ColorSensorMaxAttempts:       1,
	}

	// Reference is intentionally far away, so presence matching should fail.
	// Hybrid mode must still detect movement.
	reference := uint16(500)

	err := WaitForBallWithReferenceBaseline(s, nil, cfg, logger, nil, reference)
	if err != nil {
		t.Fatalf("expected detection from movement fallback in hybrid mode, got error: %v", err)
	}
}

func TestWaitForBallWithReferenceBaselineDelaysVibrationUntilLateRetry(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	b := &stubBuzzer{}
	logger := silentLogger()

	cfg := &config.Config{
		ColorSensorEnabled:           true,
		ColorSensorMovementThreshold: 10000,
		ColorSensorPresenceTolerance: 1,
		ColorSensorPollIntervalMs:    1,
		ColorSensorCheckDurationMs:   1,
		ColorSensorStableSamples:     1,
		ColorSensorVibrateIntensity:  0.8,
		ColorSensorVibrateDurationMs: 1,
		ColorSensorVibrateBursts:     1,
		ColorSensorMaxAttempts:       5,
	}

	reference := uint16(500)
	err := WaitForBallWithReferenceBaseline(s, b, cfg, logger, nil, reference)
	if err != ErrNoBallDetected {
		t.Fatalf("expected ErrNoBallDetected, got %v", err)
	}
	if b.count != 5 {
		t.Fatalf("expected vibration on every failed hybrid attempt, got %d", b.count)
	}
}

func TestWaitForBallIncreasesVibrationIntensityAcrossBursts(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	b := &stubBuzzer{}
	cfg := &config.Config{
		ColorSensorEnabled:           true,
		ColorSensorMovementThreshold: 10000,
		ColorSensorCheckDurationMs:   1,
		ColorSensorVibrateIntensity:  0.8,
		ColorSensorVibrateDurationMs: 1,
		ColorSensorVibrateBursts:     2,
		ColorSensorMaxAttempts:       2,
	}

	err := WaitForBall(s, b, cfg, silentLogger(), nil)
	if err != ErrNoBallDetected {
		t.Fatalf("expected ErrNoBallDetected, got %v", err)
	}
	if len(b.intensities) != 4 {
		t.Fatalf("expected 4 vibration bursts, got %d", len(b.intensities))
	}

	expected := []float64{0.8, 0.85, 0.9, 0.95}
	for i := range expected {
		if math.Abs(b.intensities[i]-expected[i]) > 0.0001 {
			t.Fatalf("expected intensity %.2f at burst %d, got %.2f", expected[i], i+1, b.intensities[i])
		}
	}
}

func TestWaitForBallWithReferenceBaselineResamplesImmediatelyAfterDriftedMiss(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	logger, buf := bufferLogger()

	cfg := &config.Config{
		ColorSensorEnabled:                        true,
		ColorSensorMovementThreshold:              10000,
		ColorSensorPresenceTolerance:              1,
		ColorSensorReferenceMaxDrift:              10,
		ColorSensorReferenceResampleAfterAttempts: 2,
		ColorSensorPollIntervalMs:                 1,
		ColorSensorCheckDurationMs:                1,
		ColorSensorStableSamples:                  1,
		ColorSensorVibrateBursts:                  0,
		ColorSensorMaxAttempts:                    2,
	}

	reference := uint16(500)
	err := WaitForBallWithReferenceBaseline(s, nil, cfg, logger, nil, reference)
	if err != ErrNoBallDetected {
		t.Fatalf("expected ErrNoBallDetected, got %v", err)
	}

	logs := buf.String()
	resampleIndex := strings.Index(logs, "resampled hybrid reference baseline")
	attemptTwoIndex := strings.Index(logs, "attempt 2/2")
	if resampleIndex == -1 {
		t.Fatal("expected resample log entry after drifted miss")
	}
	if attemptTwoIndex == -1 {
		t.Fatal("expected attempt 2 log entry")
	}
	if resampleIndex > attemptTwoIndex {
		t.Fatalf("expected resample before second attempt, logs were:\n%s", logs)
	}
	if !strings.Contains(logs, "reference drift too high") {
		t.Fatalf("expected drift log entry, logs were:\n%s", logs)
	}
}

func TestWaitForBallWithReferenceBaselineUsesMovementOnlyAfterDrift(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	logger, buf := bufferLogger()

	cfg := &config.Config{
		ColorSensorEnabled:                        true,
		ColorSensorMovementThreshold:              10000,
		ColorSensorPresenceTolerance:              18,
		ColorSensorReferenceMaxDrift:              10,
		ColorSensorReferenceResampleAfterAttempts: 2,
		ColorSensorPollIntervalMs:                 1,
		ColorSensorCheckDurationMs:                1,
		ColorSensorStableSamples:                  1,
		ColorSensorVibrateBursts:                  0,
		ColorSensorMaxAttempts:                    2,
	}

	reference := uint16(500)
	err := WaitForBallWithReferenceBaseline(s, nil, cfg, logger, nil, reference)
	if err != ErrNoBallDetected {
		t.Fatalf("expected ErrNoBallDetected, got %v", err)
	}

	logs := buf.String()
	if !strings.Contains(logs, "reference drift too high") {
		t.Fatalf("expected drift log entry, logs were:\n%s", logs)
	}
	if !strings.Contains(logs, "attempt 2/2, baseline C=") || !strings.Contains(logs, "threshold=10000") {
		t.Fatalf("expected movement-only second attempt, logs were:\n%s", logs)
	}
	secondAttemptFound := false
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "Color sensor: attempt 2/2,") {
			secondAttemptFound = true
			if !strings.Contains(line, "match_mode=movement_only") {
				t.Fatalf("expected movement-only mode for second attempt after drift, got line: %s", line)
			}
		}
	}
	if !secondAttemptFound {
		t.Fatalf("expected second attempt log line, logs were:\n%s", logs)
	}
}
