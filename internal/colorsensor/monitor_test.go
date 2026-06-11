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
	durations   []time.Duration
}

func (b *stubBuzzer) Buzz(intensity float64, duration time.Duration) error {
	b.count++
	b.intensities = append(b.intensities, intensity)
	b.durations = append(b.durations, duration)
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
	if b.count != 4 {
		t.Fatalf("expected 4 vibration bursts (1+1+2 escalation), got %d", b.count)
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
	if b.count != 9 {
		t.Fatalf("expected escalating vibration bursts across 5 attempts (1+1+2+2+3), got %d", b.count)
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

	expected := []float64{0.8, 0.83, 0.88, 0.91}
	for i := range expected {
		if math.Abs(b.intensities[i]-expected[i]) > 0.0001 {
			t.Fatalf("expected intensity %.2f at burst %d, got %.2f", expected[i], i+1, b.intensities[i])
		}
	}

	if len(b.durations) != 4 {
		t.Fatalf("expected 4 vibration durations, got %d", len(b.durations))
	}
	expectedDurations := []time.Duration{1 * time.Millisecond, 41 * time.Millisecond, 91 * time.Millisecond, 131 * time.Millisecond}
	for i := range expectedDurations {
		if b.durations[i] != expectedDurations[i] {
			t.Fatalf("expected duration %v at burst %d, got %v", expectedDurations[i], i+1, b.durations[i])
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

func TestWaitForBallWithReferenceBaselineForcesImmediateResampleOnLargeDelta(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	logger, buf := bufferLogger()

	cfg := &config.Config{
		ColorSensorEnabled:                        true,
		ColorSensorMovementThreshold:              10000,
		ColorSensorPresenceTolerance:              5,
		ColorSensorReferenceMaxDrift:              200,
		ColorSensorReferenceResampleAfterAttempts: 99,
		ColorSensorPollIntervalMs:                 1,
		ColorSensorCheckDurationMs:                1,
		ColorSensorStableSamples:                  1,
		ColorSensorVibrateBursts:                  0,
		ColorSensorMaxAttempts:                    1,
	}

	reference := uint16(500)
	err := WaitForBallWithReferenceBaseline(s, nil, cfg, logger, nil, reference)
	if err != ErrNoBallDetected {
		t.Fatalf("expected ErrNoBallDetected, got %v", err)
	}

	logs := buf.String()
	if !strings.Contains(logs, "forcing immediate reference resample after hybrid miss") {
		t.Fatalf("expected immediate resample trigger log, logs were:\n%s", logs)
	}
	if !strings.Contains(logs, "resampled hybrid reference baseline") {
		t.Fatalf("expected resample log entry despite high resample-after-attempts threshold, logs were:\n%s", logs)
	}
}

func TestWaitForBallWithReferenceBaselineReturnsToHybridAfterResample(t *testing.T) {
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
		ColorSensorMaxAttempts:                    3,
	}

	reference := uint16(500)
	err := WaitForBallWithReferenceBaseline(s, nil, cfg, logger, nil, reference)
	if err != nil {
		t.Fatalf("expected detection after hybrid mode resumes, got %v", err)
	}

	logs := buf.String()
	if !strings.Contains(logs, "reference drift too high") {
		t.Fatalf("expected drift log entry, logs were:\n%s", logs)
	}
	if !strings.Contains(logs, "attempt 2/3,") || !strings.Contains(logs, "match_mode=hybrid") {
		t.Fatalf("expected hybrid mode to resume by attempt 2 after resample, logs were:\n%s", logs)
	}
}

func TestWaitForBallWithReferenceBaselineHybridCGuardPreventsLowCFalsePositive(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	logger := silentLogger()

	cfg := &config.Config{
		ColorSensorEnabled:                        true,
		ColorSensorMovementThreshold:              1,
		ColorSensorPresenceTolerance:              10,
		ColorSensorHybridCGuardMargin:             24,
		ColorSensorReferenceMaxDrift:              5000,
		ColorSensorReferenceResampleAfterAttempts: 99,
		ColorSensorPollIntervalMs:                 1,
		ColorSensorCheckDurationMs:                5,
		ColorSensorStableSamples:                  1,
		ColorSensorVibrateBursts:                  0,
		ColorSensorMaxAttempts:                    1,
	}

	// This reference intentionally puts the simulated readings far below the
	// hybrid C-guard floor. Movement-only spikes must not count as detection.
	reference := uint16(1000)
	err := WaitForBallWithReferenceBaseline(s, nil, cfg, logger, nil, reference)
	if err != ErrNoBallDetected {
		t.Fatalf("expected ErrNoBallDetected when below C guard floor, got %v", err)
	}
}
