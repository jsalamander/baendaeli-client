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
	if !s.IsEnabled() {
		logger.Println("Color sensor disabled, skipping ball detection")
		return nil
	}

	pollInterval := time.Duration(cfg.ColorSensorPollIntervalMs) * time.Millisecond
	checkDuration := time.Duration(cfg.ColorSensorCheckDurationMs) * time.Millisecond
	vibrateDuration := time.Duration(cfg.ColorSensorVibrateDurationMs) * time.Millisecond
	pauseBetweenBursts := 300 * time.Millisecond

	for attempt := 1; attempt <= cfg.ColorSensorMaxAttempts; attempt++ {
		if observer != nil {
			observer(attempt, cfg.ColorSensorMaxAttempts)
		}

		baseline, err := baseline(s, logger)
		if err != nil {
			logger.Printf("Color sensor: could not read baseline (attempt %d/%d): %v", attempt, cfg.ColorSensorMaxAttempts, err)
			// treat as no detection on read error and carry on
		}

		logger.Printf("Color sensor: attempt %d/%d, baseline C=%d, threshold=%d", attempt, cfg.ColorSensorMaxAttempts, baseline, cfg.ColorSensorMovementThreshold)

		if detected := pollForMovement(s, baseline, cfg.ColorSensorMovementThreshold, checkDuration, pollInterval, logger); detected {
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
func pollForMovement(s *Sensor, baseline uint16, threshold int, window, interval time.Duration, logger *log.Logger) bool {
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		c, _, _, _, err := s.Read()
		if err != nil {
			logger.Printf("Color sensor: read error during polling: %v", err)
			time.Sleep(interval)
			continue
		}
		diff := int(c) - int(baseline)
		if diff < 0 {
			diff = -diff
		}
		if diff >= threshold {
			return true
		}
		time.Sleep(interval)
	}
	return false
}
