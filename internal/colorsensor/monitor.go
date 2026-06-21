package colorsensor

import (
	"errors"
	"log"
	"math"
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
	detectMode        detectMode
}

type detectMode int

const (
	detectModeMovementOnly detectMode = iota
	detectModeHybridReference
	detectModePresenceReference
)

// WaitForBall monitors the colour sensor and waits until a ball drop is detected.
// It uses the clear channel (C) to detect movement: a ball passing the sensor causes
// a significant change in the ambient light reading.
//
// On each attempt it:
//  1. Establishes a baseline C value (average of 3 readings).
//  2. Polls the sensor every 200 ms for up to CheckDurationMs.
//  3. If any reading exceeds baseline + MovementThreshold → ball detected, return nil.
//  4. If the window expires → optionally fires vibration bursts before a later retry.
//  5. Repeats up to MaxAttempts total.
//
// Returns ErrNoBallDetected if no ball is detected after all attempts.
func WaitForBall(s *Sensor, vib vibratorBuzzer, cfg *config.Config, logger *log.Logger, observer AttemptObserver) error {
	return waitForBallWithOptions(s, vib, cfg, logger, observer, detectOptions{})
}

// WaitForBallWithReferenceBaseline uses a pre-dispense baseline to detect balls that have
// already come to rest by the time polling starts.
//
// Detection is hybrid when a reference is provided:
// - movement hit: absolute delta to current baseline >= movement threshold
// - presence hit: absolute delta to reference baseline <= presence tolerance
func WaitForBallWithReferenceBaseline(s *Sensor, vib vibratorBuzzer, cfg *config.Config, logger *log.Logger, observer AttemptObserver, referenceBaseline uint16) error {
	return waitForBallWithOptions(s, vib, cfg, logger, observer, detectOptions{referenceBaseline: &referenceBaseline, detectMode: detectModeHybridReference})
}

// WaitForBallWithPresenceReferenceBaseline detects a settled ball by checking
// that sensor readings stay close to a known ball-present reference baseline.
func WaitForBallWithPresenceReferenceBaseline(s *Sensor, vib vibratorBuzzer, cfg *config.Config, logger *log.Logger, observer AttemptObserver, referenceBaseline uint16) error {
	return waitForBallWithOptions(s, vib, cfg, logger, observer, detectOptions{referenceBaseline: &referenceBaseline, detectMode: detectModePresenceReference})
}

func waitForBallWithOptions(s *Sensor, vib vibratorBuzzer, cfg *config.Config, logger *log.Logger, observer AttemptObserver, opts detectOptions) error {
	if !s.IsEnabled() {
		logger.Println("Color sensor disabled, skipping ball detection")
		return nil
	}

	pollInterval := time.Duration(cfg.ColorSensorPollIntervalMs) * time.Millisecond
	checkDuration := time.Duration(cfg.ColorSensorCheckDurationMs) * time.Millisecond
	clearBandWindow := time.Duration(cfg.ColorSensorClearBandWindowMs) * time.Millisecond
	if clearBandWindow <= 0 {
		clearBandWindow = checkDuration
	}
	if clearBandWindow > checkDuration {
		clearBandWindow = checkDuration
	}
	settleDelay := time.Duration(cfg.ColorSensorSettleDelayMs) * time.Millisecond
	stableSamples := cfg.ColorSensorStableSamples
	if stableSamples < 1 {
		stableSamples = 1
	}
	referenceMaxDrift := cfg.ColorSensorReferenceMaxDrift
	if referenceMaxDrift <= 0 {
		referenceMaxDrift = 35
	}
	referenceResampleAfterAttempts := cfg.ColorSensorReferenceResampleAfterAttempts
	if referenceResampleAfterAttempts <= 0 {
		referenceResampleAfterAttempts = 2
	}

	activeReference := opts.referenceBaseline
	failedReferenceAttempts := 0
	forceMovementOnly := false
	for attempt := 1; attempt <= cfg.ColorSensorMaxAttempts; attempt++ {
		if observer != nil {
			observer(attempt, cfg.ColorSensorMaxAttempts)
		}

		baselineValue, err := baseline(s, logger)
		if err != nil {
			logger.Printf("Color sensor: could not read baseline (attempt %d/%d): %v", attempt, cfg.ColorSensorMaxAttempts, err)
			// treat as no detection on read error and carry on
		}

		if settleDelay > 0 {
			time.Sleep(settleDelay)
		}

		if cfg.ColorSensorClearBandEnabled {
			if cfg.ColorSensorClearJamMax > 0 && cfg.ColorSensorClearBallMin > cfg.ColorSensorClearJamMax {
				logger.Printf("Color sensor: attempt %d/%d clear-band precheck jam_max=%d ball_min=%d stable_samples=%d window_ms=%d", attempt, cfg.ColorSensorMaxAttempts, cfg.ColorSensorClearJamMax, cfg.ColorSensorClearBallMin, stableSamples, clearBandWindow.Milliseconds())
				if detected := pollForClearBandPresence(s, cfg.ColorSensorClearJamMax, cfg.ColorSensorClearBallMin, stableSamples, clearBandWindow, pollInterval, cfg.ColorSensorDebugLogging, logger); detected {
					logger.Printf("Color sensor: ball detected on attempt %d by clear-band precheck", attempt)
					return nil
				}
			} else {
				logger.Printf("Color sensor: attempt %d/%d clear-band precheck skipped due to invalid thresholds (jam_max=%d ball_min=%d)", attempt, cfg.ColorSensorMaxAttempts, cfg.ColorSensorClearJamMax, cfg.ColorSensorClearBallMin)
			}
		}

		referenceForAttempt := activeReference
		attemptMode := opts.detectMode
		if forceMovementOnly {
			attemptMode = detectModeMovementOnly
		}
		if referenceForAttempt != nil && opts.detectMode == detectModeHybridReference {
			if absInt(int(baselineValue)-int(*referenceForAttempt)) > referenceMaxDrift {
				logger.Printf("Color sensor: attempt %d/%d reference drift too high (baseline=%d reference=%d max_drift=%d), temporarily ignoring reference", attempt, cfg.ColorSensorMaxAttempts, baselineValue, *referenceForAttempt, referenceMaxDrift)
				referenceForAttempt = nil
				attemptMode = detectModeMovementOnly
				forceMovementOnly = true
			}
		}

		if referenceForAttempt != nil {
			switch attemptMode {
			case detectModePresenceReference:
				logger.Printf("Color sensor: attempt %d/%d, baseline C=%d, reference C=%d, match_mode=near_reference, movement_threshold=%d, presence_tolerance=%d, stable_samples=%d", attempt, cfg.ColorSensorMaxAttempts, baselineValue, *referenceForAttempt, cfg.ColorSensorMovementThreshold, cfg.ColorSensorPresenceTolerance, stableSamples)
			case detectModeHybridReference:
				logger.Printf("Color sensor: attempt %d/%d, baseline C=%d, reference C=%d, match_mode=hybrid, movement_threshold=%d, presence_tolerance=%d, c_guard_margin=%d, stable_samples=%d", attempt, cfg.ColorSensorMaxAttempts, baselineValue, *referenceForAttempt, cfg.ColorSensorMovementThreshold, cfg.ColorSensorPresenceTolerance, cfg.ColorSensorHybridCGuardMargin, stableSamples)
			default:
				logger.Printf("Color sensor: attempt %d/%d, baseline C=%d, reference C=%d, match_mode=movement_only, movement_threshold=%d, stable_samples=%d", attempt, cfg.ColorSensorMaxAttempts, baselineValue, *referenceForAttempt, cfg.ColorSensorMovementThreshold, stableSamples)
			}
		} else {
			logger.Printf("Color sensor: attempt %d/%d, baseline C=%d, threshold=%d, stable_samples=%d", attempt, cfg.ColorSensorMaxAttempts, baselineValue, cfg.ColorSensorMovementThreshold, stableSamples)
		}

		if detected := pollForMovement(s, baselineValue, referenceForAttempt, attemptMode, cfg.ColorSensorMovementThreshold, cfg.ColorSensorPresenceTolerance, cfg.ColorSensorHybridCGuardMargin, stableSamples, checkDuration, pollInterval, cfg.ColorSensorDebugLogging, logger); detected {
			logger.Printf("Color sensor: ball detected on attempt %d", attempt)
			return nil
		}

		if activeReference != nil && opts.detectMode == detectModeHybridReference {
			baselineReferenceDelta := absInt(int(baselineValue) - int(*activeReference))
			if forceMovementOnly {
				// Drifted references are ignored for this detection cycle. Avoid
				// learning a new reference from an empty/jammed miss window.
				failedReferenceAttempts = 0
			} else {
				forceImmediateResample := baselineReferenceDelta > cfg.ColorSensorPresenceTolerance
				if forceImmediateResample {
					logger.Printf("Color sensor: forcing immediate reference resample after hybrid miss (baseline=%d reference=%d delta=%d presence_tolerance=%d)", baselineValue, *activeReference, baselineReferenceDelta, cfg.ColorSensorPresenceTolerance)
				}
				failedReferenceAttempts++
				if forceImmediateResample || failedReferenceAttempts >= referenceResampleAfterAttempts {
					resampledReference, resampleErr := baseline(s, logger)
					if resampleErr != nil {
						logger.Printf("Color sensor: failed to resample reference baseline after %d failed attempts: %v", failedReferenceAttempts, resampleErr)
					} else {
						activeReference = &resampledReference
						failedReferenceAttempts = 0
						forceMovementOnly = false
						logger.Printf("Color sensor: resampled hybrid reference baseline C=%d", resampledReference)
					}
				}
			}
		}

		bursts := vibrationBurstsForAttempt(cfg.ColorSensorVibrateBursts, attempt)
		logger.Printf("Color sensor: no ball detected in window (attempt %d/%d), vibrating %d bursts", attempt, cfg.ColorSensorMaxAttempts, bursts)
		if vib != nil {
			for burst := 0; burst < bursts; burst++ {
				intensity := scaledVibrationIntensity(cfg.ColorSensorVibrateIntensity, attempt, burst)
				duration := scaledVibrationDuration(time.Duration(cfg.ColorSensorVibrateDurationMs)*time.Millisecond, attempt, burst)
				pauseBetweenBursts := scaledVibrationPause(attempt)
				if err := vib.Buzz(intensity, duration); err != nil {
					logger.Printf("Color sensor: vibration burst %d failed: %v", burst+1, err)
				} else if cfg.ColorSensorDebugLogging {
					logger.Printf("Color sensor: vibration burst %d intensity=%.2f duration_ms=%d pause_ms=%d", burst+1, intensity, duration.Milliseconds(), pauseBetweenBursts.Milliseconds())
				}
				time.Sleep(pauseBetweenBursts)
			}
		}
	}

	return ErrNoBallDetected
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func vibrationBurstsForAttempt(base int, attempt int) int {
	if base <= 0 {
		return 0
	}
	bursts := base + (attempt-1)/2
	if bursts > 5 {
		return 5
	}
	return bursts
}

func scaledVibrationIntensity(base float64, attempt int, burst int) float64 {
	if base <= 0 {
		return 0
	}
	return math.Min(1.0, base+0.08*float64(attempt-1)+0.03*float64(burst))
}

func scaledVibrationDuration(base time.Duration, attempt int, burst int) time.Duration {
	if base <= 0 {
		base = 100 * time.Millisecond
	}
	duration := base + time.Duration(90*(attempt-1)+40*burst)*time.Millisecond
	maxDuration := 700 * time.Millisecond
	if duration > maxDuration {
		return maxDuration
	}
	return duration
}

func scaledVibrationPause(attempt int) time.Duration {
	pause := 300 - 30*(attempt-1)
	if pause < 150 {
		pause = 150
	}
	return time.Duration(pause) * time.Millisecond
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

// pollForMovement polls the sensor until movement or presence detection is stable
// or the window expires.
func pollForMovement(s *Sensor, baseline uint16, referenceBaseline *uint16, mode detectMode, movementThreshold int, presenceTolerance int, cGuardMargin int, stableSamples int, window, interval time.Duration, debug bool, logger *log.Logger) bool {
	deadline := time.Now().Add(window)
	consecutiveHits := 0
	sampleIndex := 0
	if presenceTolerance <= 0 {
		presenceTolerance = movementThreshold
	}
	if cGuardMargin < 0 {
		cGuardMargin = 0
	}
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
		movementHit := diffCurrent >= movementThreshold
		presenceHit := false
		cGuardPass := true
		cGuardFloor := -1
		if referenceBaseline != nil {
			diffReference = int(c) - int(*referenceBaseline)
			if diffReference < 0 {
				diffReference = -diffReference
			}
			presenceHit = diffReference <= presenceTolerance
			if mode == detectModeHybridReference && cGuardMargin > 0 {
				cGuardFloor = int(*referenceBaseline) - cGuardMargin
				cGuardPass = int(c) >= cGuardFloor
			}
		}

		hit := movementHit
		if referenceBaseline != nil {
			switch mode {
			case detectModePresenceReference:
				hit = presenceHit
			case detectModeHybridReference:
				hit = presenceHit || (movementHit && cGuardPass)
			default:
				hit = movementHit
			}
		}

		if hit {
			consecutiveHits++
		} else {
			consecutiveHits = 0
		}

		if debug {
			if diffReference >= 0 {
				if cGuardFloor >= 0 {
					logger.Printf("Color sensor debug: sample=%d C=%d baseline=%d reference=%d diff_current=%d diff_reference=%d movement_hit=%t presence_hit=%t c_guard_pass=%t c_guard_floor=%d mode=%d movement_threshold=%d presence_tolerance=%d c_guard_margin=%d consecutive_hits=%d/%d", sampleIndex, c, baseline, *referenceBaseline, diffCurrent, diffReference, movementHit, presenceHit, cGuardPass, cGuardFloor, mode, movementThreshold, presenceTolerance, cGuardMargin, consecutiveHits, stableSamples)
				} else {
					logger.Printf("Color sensor debug: sample=%d C=%d baseline=%d reference=%d diff_current=%d diff_reference=%d movement_hit=%t presence_hit=%t mode=%d movement_threshold=%d presence_tolerance=%d consecutive_hits=%d/%d", sampleIndex, c, baseline, *referenceBaseline, diffCurrent, diffReference, movementHit, presenceHit, mode, movementThreshold, presenceTolerance, consecutiveHits, stableSamples)
				}
			} else {
				logger.Printf("Color sensor debug: sample=%d C=%d baseline=%d diff_current=%d movement_hit=%t movement_threshold=%d consecutive_hits=%d/%d", sampleIndex, c, baseline, diffCurrent, movementHit, movementThreshold, consecutiveHits, stableSamples)
			}
		}

		if consecutiveHits >= stableSamples {
			return true
		}
		time.Sleep(interval)
	}
	return false
}

func pollForClearBandPresence(s *Sensor, jamMax int, ballMin int, stableSamples int, window, interval time.Duration, debug bool, logger *log.Logger) bool {
	deadline := time.Now().Add(window)
	consecutiveBallHits := 0
	consecutiveJamHits := 0
	sampleIndex := 0

	for time.Now().Before(deadline) {
		sampleIndex++
		c, _, _, _, err := s.Read()
		if err != nil {
			logger.Printf("Color sensor: read error during clear-band precheck: %v", err)
			time.Sleep(interval)
			continue
		}

		cValue := int(c)
		ballHit := cValue >= ballMin
		jamHit := cValue <= jamMax
		switch {
		case ballHit:
			consecutiveBallHits++
			consecutiveJamHits = 0
		case jamHit:
			consecutiveJamHits++
			consecutiveBallHits = 0
		default:
			consecutiveBallHits = 0
			consecutiveJamHits = 0
		}

		if debug {
			logger.Printf("Color sensor debug: clear-band sample=%d C=%d jam_max=%d ball_min=%d ball_hit=%t jam_hit=%t consecutive_ball_hits=%d/%d consecutive_jam_hits=%d/%d", sampleIndex, cValue, jamMax, ballMin, ballHit, jamHit, consecutiveBallHits, stableSamples, consecutiveJamHits, stableSamples)
		}

		if consecutiveBallHits >= stableSamples {
			return true
		}
		if consecutiveJamHits >= stableSamples {
			return false
		}

		time.Sleep(interval)
	}

	return false
}
