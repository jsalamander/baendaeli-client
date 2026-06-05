package colorsensor

import (
	"errors"
	"log"
	"time"

	"github.com/jsalamander/baendaeli-client/internal/config"
)

// ErrNoBallDetected is returned when no ball drop is detected after all attempts are exhausted.
var ErrNoBallDetected = errors.New("no ball detected after max attempts")

// vibratorBuzzer is a narrow interface so monitor.go doesn't import the vibrator package directly.
type vibratorBuzzer interface {
	Buzz(intensity float64, duration time.Duration) error
}

// AttemptObserver is called at the beginning of each detection attempt.
// attempt is 1-based and maxAttempts is the configured total.
type AttemptObserver func(attempt int, maxAttempts int)

type detectOptions struct {
	referenceBaseline *uint16
	matchReference    bool
}

// WaitForBall monitors the colour sensor and waits until a ball drop is detected.
// It uses the clear channel (C) to detect movement: a ball passing the sensor causes
// a significant change in the ambient light reading.
//
// On each attempt it:
//  1. Establishes a baseline C value (average of 3 readings).
//  2. Polls the sensor every 200 ms for up to CheckDurationMs.
//  3. If any reading exceeds baseline + MovementThreshold → ball detected, return nil.
//  4. If the window expires → fires VibrateBursts vibration bursts to dislodge a jam.
//  5. Repeats up to MaxAttempts total.
//
// Returns ErrNoBallDetected if no ball is detected after all attempts.
func WaitForBall(s *Sensor, vib vibratorBuzzer, cfg *config.Config, logger *log.Logger, observer AttemptObserver) error {
	return waitForBallWithOptions(s, vib, cfg, logger, observer, detectOptions{})
}

// WaitForBallWithReferenceBaseline uses a pre-dispense baseline to detect balls that have
// already come to rest by the time polling starts.
func WaitForBallWithReferenceBaseline(s *Sensor, vib vibratorBuzzer, cfg *config.Config, logger *log.Logger, observer AttemptObserver, referenceBaseline uint16) error {
	return waitForBallWithOptions(s, vib, cfg, logger, observer, detectOptions{referenceBaseline: &referenceBaseline})
}

// WaitForBallWithPresenceReferenceBaseline detects a settled ball by checking
// that sensor readings stay close to a known ball-present reference baseline.
func WaitForBallWithPresenceReferenceBaseline(s *Sensor, vib vibratorBuzzer, cfg *config.Config, logger *log.Logger, observer AttemptObserver, referenceBaseline uint16) error {
	return waitForBallWithOptions(s, vib, cfg, logger, observer, detectOptions{referenceBaseline: &referenceBaseline, matchReference: true})
}

func waitForBallWithOptions(s *Sensor, vib vibratorBuzzer, cfg *config.Config, logger *log.Logger, observer AttemptObserver, opts detectOptions) error {
	if !s.IsEnabled() {
		logger.Println("Color sensor disabled, skipping ball detection")
		return nil
	}

	pollInterval := time.Duration(cfg.ColorSensorPollIntervalMs) * time.Millisecond
	checkDuration := time.Duration(cfg.ColorSensorCheckDurationMs) * time.Millisecond
	settleDelay := time.Duration(cfg.ColorSensorSettleDelayMs) * time.Millisecond
	vibrateDuration := time.Duration(cfg.ColorSensorVibrateDurationMs) * time.Millisecond
	pauseBetweenBursts := 300 * time.Millisecond
	stableSamples := cfg.ColorSensorStableSamples
	if stableSamples < 1 {
		stableSamples = 1
	}

	for attempt := 1; attempt <= cfg.ColorSensorMaxAttempts; attempt++ {
		if observer != nil {
			observer(attempt, cfg.ColorSensorMaxAttempts)
		}

		baseline, err := baseline(s, logger)
		if err != nil {
			logger.Printf("Color sensor: could not read baseline (attempt %d/%d): %v", attempt, cfg.ColorSensorMaxAttempts, err)
			// treat as no detection on read error and carry on
		}

		if settleDelay > 0 {
			time.Sleep(settleDelay)
		}

		if opts.referenceBaseline != nil {
			if opts.matchReference {
				logger.Printf("Color sensor: attempt %d/%d, baseline C=%d, reference C=%d, match_mode=near_reference, threshold=%d, stable_samples=%d", attempt, cfg.ColorSensorMaxAttempts, baseline, *opts.referenceBaseline, cfg.ColorSensorMovementThreshold, stableSamples)
			} else {
				logger.Printf("Color sensor: attempt %d/%d, baseline C=%d, reference C=%d, match_mode=difference, threshold=%d, stable_samples=%d", attempt, cfg.ColorSensorMaxAttempts, baseline, *opts.referenceBaseline, cfg.ColorSensorMovementThreshold, stableSamples)
			}
		} else {
			logger.Printf("Color sensor: attempt %d/%d, baseline C=%d, threshold=%d, stable_samples=%d", attempt, cfg.ColorSensorMaxAttempts, baseline, cfg.ColorSensorMovementThreshold, stableSamples)
		}

		if detected := pollForMovement(s, baseline, opts.referenceBaseline, opts.matchReference, cfg.ColorSensorMovementThreshold, stableSamples, checkDuration, pollInterval, cfg.ColorSensorDebugLogging, logger); detected {
			logger.Printf("Color sensor: ball detected on attempt %d", attempt)
			return nil
		}

		logger.Printf("Color sensor: no ball detected in window (attempt %d/%d), vibrating %d bursts", attempt, cfg.ColorSensorMaxAttempts, cfg.ColorSensorVibrateBursts)
		if vib != nil {
			for burst := 0; burst < cfg.ColorSensorVibrateBursts; burst++ {
				if err := vib.Buzz(cfg.ColorSensorVibrateIntensity, vibrateDuration); err != nil {
					logger.Printf("Color sensor: vibration burst %d failed: %v", burst+1, err)
				}
				time.Sleep(pauseBetweenBursts)
			}
		}
	}

	return ErrNoBallDetected
}

// SampleBaseline returns the average clear-channel reading over 3 samples.
func SampleBaseline(s *Sensor, logger *log.Logger) (uint16, error) {
	return baseline(s, logger)
}

// baseline returns the average clear-channel reading over 3 samples.
func baseline(s *Sensor, logger *log.Logger) (uint16, error) {
	const samples = 3
	var sum uint64
	for i := 0; i < samples; i++ {
		c, _, _, _, err := s.Read()
		if err != nil {
			return 0, err
		}
		sum += uint64(c)
		time.Sleep(50 * time.Millisecond)
	}
	return uint16(sum / samples), nil
}

// pollForMovement polls the sensor until movement is detected or the window expires.
func pollForMovement(s *Sensor, baseline uint16, referenceBaseline *uint16, matchReference bool, threshold int, stableSamples int, window, interval time.Duration, debug bool, logger *log.Logger) bool {
	deadline := time.Now().Add(window)
	consecutiveHits := 0
	sampleIndex := 0
	for time.Now().Before(deadline) {
		sampleIndex++
		c, _, _, _, err := s.Read()
		if err != nil {
			logger.Printf("Color sensor: read error during polling: %v", err)
			time.Sleep(interval)
			continue
		}

		diffCurrent := int(c) - int(baseline)
		if diffCurrent < 0 {
			diffCurrent = -diffCurrent
		}

		diffReference := -1
		effectiveDiff := diffCurrent
		if referenceBaseline != nil {
			diffReference = int(c) - int(*referenceBaseline)
			if diffReference < 0 {
				diffReference = -diffReference
			}
			if !matchReference && diffReference > effectiveDiff {
				effectiveDiff = diffReference
			}
		}

		if matchReference && referenceBaseline != nil {
			if diffReference <= threshold {
				consecutiveHits++
			} else {
				consecutiveHits = 0
			}
		} else {
			if effectiveDiff >= threshold {
				consecutiveHits++
			} else {
				consecutiveHits = 0
			}
		}

		if debug {
			if diffReference >= 0 {
				if matchReference {
					logger.Printf("Color sensor debug: sample=%d C=%d baseline=%d reference=%d diff_current=%d diff_reference=%d match_mode=near_reference threshold=%d consecutive_hits=%d/%d", sampleIndex, c, baseline, *referenceBaseline, diffCurrent, diffReference, threshold, consecutiveHits, stableSamples)
				} else {
					logger.Printf("Color sensor debug: sample=%d C=%d baseline=%d reference=%d diff_current=%d diff_reference=%d effective_diff=%d threshold=%d consecutive_hits=%d/%d", sampleIndex, c, baseline, *referenceBaseline, diffCurrent, diffReference, effectiveDiff, threshold, consecutiveHits, stableSamples)
				}
			} else {
				logger.Printf("Color sensor debug: sample=%d C=%d baseline=%d diff_current=%d threshold=%d consecutive_hits=%d/%d", sampleIndex, c, baseline, diffCurrent, threshold, consecutiveHits, stableSamples)
			}
		}

		if consecutiveHits >= stableSamples {
			return true
		}
		time.Sleep(interval)
	}
	return false
}
