package actuator

import (
	"fmt"
	"log"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/host/v3"
)

// Homing duration - fixed time to ensure full retraction from any position
const homingDuration = 10 * time.Second

// Settling delay to ensure motor fully stops before direction change
// This prevents momentum from affecting next movement
const settlingDelay = 100 * time.Millisecond

type Config struct {
	Enabled      bool
	ENAPin       string // e.g., "GPIO25"
	IN1Pin       string // e.g., "GPIO8"
	IN2Pin       string // e.g., "GPIO7"
	MovementTime int    // seconds - MUST be identical for extend and retract
	PauseTime    int    // seconds, pause between extend and retract
}

type ActuateResult struct {
	Status      string `json:"status"`
	TotalTimeMs int    `json:"total_time_ms"`
	Error       string `json:"error,omitempty"`
}

type Actuator struct {
	enabled      bool
	enaPin       gpio.PinOut
	in1Pin       gpio.PinOut
	in2Pin       gpio.PinOut
	movementTime time.Duration // Identical for extend and retract
	pause        time.Duration
	isHome       bool // Track if actuator is at home position
}

var actuator *Actuator

// Init initializes GPIO and the actuator control pins
func Init(config Config) error {
	if !config.Enabled {
		log.Println("Actuator control disabled")
		return nil
	}

	// Enforce identical movement time for extend and retract
	if config.MovementTime == 0 {
		config.MovementTime = 2
	}
	if config.PauseTime == 0 {
		config.PauseTime = 2
	}

	log.Printf("Actuator config: movement_time=%ds (extend=retract), pause=%ds", 
		config.MovementTime, config.PauseTime)

	// Initialize periph/x host
	if _, err := host.Init(); err != nil {
		return fmt.Errorf("failed to init periph host: %w", err)
	}

	// Open pins
	enaPin := gpioreg.ByName(config.ENAPin)
	if enaPin == nil {
		return fmt.Errorf("failed to open ENA pin %s", config.ENAPin)
	}

	in1Pin := gpioreg.ByName(config.IN1Pin)
	if in1Pin == nil {
		return fmt.Errorf("failed to open IN1 pin %s", config.IN1Pin)
	}

	in2Pin := gpioreg.ByName(config.IN2Pin)
	if in2Pin == nil {
		return fmt.Errorf("failed to open IN2 pin %s", config.IN2Pin)
	}

	actuator = &Actuator{
		enabled:      true,
		enaPin:       enaPin,
		in1Pin:       in1Pin,
		in2Pin:       in2Pin,
		movementTime: time.Duration(config.MovementTime) * time.Second,
		pause:        time.Duration(config.PauseTime) * time.Second,
		isHome:       false, // Will be set to true after homing completes
	}

	// Set ENA pin HIGH to enable the actuator
	if err := actuator.enaPin.Out(gpio.High); err != nil {
		return fmt.Errorf("failed to set ENA pin high: %w", err)
	}

	log.Println("Actuator initialized successfully (homing will run in background)")
	return nil
}

// stopMotor ensures motor fully stops with settling delay to prevent momentum
func (a *Actuator) stopMotor() error {
	if err := a.in1Pin.Out(gpio.Low); err != nil {
		return fmt.Errorf("failed to set IN1 low: %w", err)
	}
	if err := a.in2Pin.Out(gpio.Low); err != nil {
		return fmt.Errorf("failed to set IN2 low: %w", err)
	}
	// Settling delay to ensure motor completely stops before next operation
	time.Sleep(settlingDelay)
	return nil
}

// preciseDelay uses a timer for more accurate timing than time.Sleep
func preciseDelay(d time.Duration) {
	timer := time.NewTimer(d)
	<-timer.C
}

// Home retracts the actuator to the shortest position (home position)
// Should be called after server startup to avoid blocking
func Home() {
	if actuator == nil || !actuator.enabled {
		log.Println("Actuator: skipping homing (not initialized or disabled)")
		return
	}

	// Retract to shortest position on startup (home position)
	// Run for fixed 10 seconds to ensure full retraction regardless of starting position
	log.Println("Actuator: retracting to home position...")
	if err := actuator.in1Pin.Out(gpio.Low); err != nil {
		log.Printf("Actuator homing error: failed to set IN1 low: %v", err)
		return
	}
	if err := actuator.in2Pin.Out(gpio.High); err != nil {
		log.Printf("Actuator homing error: failed to set IN2 high: %v", err)
		return
	}
	
	// Run retract for fixed duration to guarantee full retraction
	log.Printf("Actuator: homing for %v", homingDuration)
	time.Sleep(homingDuration)
	
	// Stop: both LOW
	if err := actuator.in1Pin.Out(gpio.Low); err != nil {
		log.Printf("Actuator homing error: failed to set IN1 low after homing: %v", err)
		return
	}
	if err := actuator.in2Pin.Out(gpio.Low); err != nil {
		log.Printf("Actuator homing error: failed to set IN2 low after homing: %v", err)
		return
	}

	// Wait for motor to fully stop (settling time)
	time.Sleep(settlingDelay)
	
	actuator.isHome = true
	log.Println("Actuator: homing complete - now at home position")
}

// Trigger executes one extend-pause-retract cycle with precise equal timing
// CRITICAL: Uses identical duration for extend and retract to ensure equal movement
func (a *Actuator) Trigger() (int, error) {
	start := time.Now()
	if !a.enabled {
		// Mock: wait for the configured time (2x movement + pause + settling)
		mockDuration := 2*a.movementTime + a.pause + 2*settlingDelay
		time.Sleep(mockDuration)
		return int(mockDuration.Milliseconds()), nil
	}

	if !a.isHome {
		log.Println("Warning: actuator not at home position before trigger")
	}

	log.Printf("Actuator: extending for exactly %v...", a.movementTime)
	// Extend: IN1 HIGH, IN2 LOW
	if err := a.in1Pin.Out(gpio.High); err != nil {
		return 0, fmt.Errorf("failed to set IN1 high: %w", err)
	}
	if err := a.in2Pin.Out(gpio.Low); err != nil {
		return 0, fmt.Errorf("failed to set IN2 low: %w", err)
	}

	// Use precise timing for extend
	preciseDelay(a.movementTime)

	// Stop and settle before direction change
	if err := a.stopMotor(); err != nil {
		return 0, fmt.Errorf("failed to stop after extend: %w", err)
	}
	a.isHome = false

	log.Printf("Actuator: pausing for %v...", a.pause)
	preciseDelay(a.pause)

	log.Printf("Actuator: retracting for exactly %v (same as extend)...", a.movementTime)
	// Retract: IN1 LOW, IN2 HIGH
	if err := a.in1Pin.Out(gpio.Low); err != nil {
		return 0, fmt.Errorf("failed to set IN1 low: %w", err)
	}
	if err := a.in2Pin.Out(gpio.High); err != nil {
		return 0, fmt.Errorf("failed to set IN2 high: %w", err)
	}

	// Use identical precise timing for retract (CRITICAL for equal movement)
	preciseDelay(a.movementTime)

	// Stop and settle - back at home position
	if err := a.stopMotor(); err != nil {
		return 0, fmt.Errorf("failed to stop after retract: %w", err)
	}
	a.isHome = true

	totalMs := int(time.Since(start).Milliseconds())
	log.Printf("Actuator cycle complete: extend=%v, retract=%v (identical), total=%dms", 
		a.movementTime, a.movementTime, totalMs)
	return totalMs, nil
}

// Trigger calls the actuator if initialized and returns timing info
func Trigger() (int, error) {
	if actuator == nil {
		// No actuator configured; return mock timing (2+2+2 = 6 seconds)
		time.Sleep(6 * time.Second)
		return 6000, nil
	}
	return actuator.Trigger()
}

// Cleanup closes GPIO resources
func Cleanup() {
	if actuator != nil {
		actuator.enaPin.Out(gpio.Low)
		actuator.enaPin.Halt()
		actuator.in1Pin.Halt()
		actuator.in2Pin.Halt()
		log.Println("Actuator GPIO cleaned up")
	}
}
