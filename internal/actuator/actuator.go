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

type Config struct {
	Enabled     bool
	ENAPin      string // e.g., "GPIO25"
	IN1Pin      string // e.g., "GPIO8"
	IN2Pin      string // e.g., "GPIO7"
	ExtendTime  int    // seconds
	RetractTime int    // seconds
	PauseTime   int    // seconds, pause between extend and retract
}

type ActuateResult struct {
	Status      string `json:"status"`
	TotalTimeMs int    `json:"total_time_ms"`
	Error       string `json:"error,omitempty"`
}

type Actuator struct {
	enabled bool
	enaPin  gpio.PinOut
	in1Pin  gpio.PinOut
	in2Pin  gpio.PinOut
	extend  time.Duration
	retract time.Duration
	pause   time.Duration
}

var actuator *Actuator

// Init initializes GPIO and the actuator control pins
func Init(config Config) error {
	if !config.Enabled {
		log.Println("Actuator control disabled")
		return nil
	}

	if config.ExtendTime == 0 {
		config.ExtendTime = 2
	}
	if config.RetractTime == 0 {
		config.RetractTime = 2
	}
	if config.PauseTime == 0 {
		config.PauseTime = 2
	}

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
		enabled: true,
		enaPin:  enaPin,
		in1Pin:  in1Pin,
		in2Pin:  in2Pin,
		extend:  time.Duration(config.ExtendTime) * time.Second,
		retract: time.Duration(config.RetractTime) * time.Second,
		pause:   time.Duration(config.PauseTime) * time.Second,
	}

	// Set ENA pin HIGH to enable the actuator
	if err := actuator.enaPin.Out(gpio.High); err != nil {
		return fmt.Errorf("failed to set ENA pin high: %w", err)
	}

	log.Println("Actuator initialized successfully (homing will run in background)")
	return nil
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

	log.Println("Actuator: homing complete")
}

// Trigger executes one extend-pause-retract cycle and returns timing info
func (a *Actuator) Trigger() (int, error) {
	start := time.Now()
	if !a.enabled {
		// Mock: wait for the configured time
		mockDuration := a.extend + a.pause + a.retract
		time.Sleep(mockDuration)
		return int(mockDuration.Milliseconds()), nil
	}

	log.Println("Actuator: extending...")
	// Extend: IN1 HIGH, IN2 LOW
	if err := a.in1Pin.Out(gpio.High); err != nil {
		return 0, fmt.Errorf("failed to set IN1 high: %w", err)
	}
	if err := a.in2Pin.Out(gpio.Low); err != nil {
		return 0, fmt.Errorf("failed to set IN2 low: %w", err)
	}

	time.Sleep(a.extend)

	log.Println("Actuator: pausing...")
	time.Sleep(a.pause)

	log.Println("Actuator: retracting...")
	// Retract: IN1 LOW, IN2 HIGH
	if err := a.in1Pin.Out(gpio.Low); err != nil {
		return 0, fmt.Errorf("failed to set IN1 low: %w", err)
	}
	if err := a.in2Pin.Out(gpio.High); err != nil {
		return 0, fmt.Errorf("failed to set IN2 high: %w", err)
	}

	time.Sleep(a.retract)

	// Stop: both LOW
	if err := a.in1Pin.Out(gpio.Low); err != nil {
		return 0, fmt.Errorf("failed to set IN1 low: %w", err)
	}
	if err := a.in2Pin.Out(gpio.Low); err != nil {
		return 0, fmt.Errorf("failed to set IN2 low: %w", err)
	}

	totalMs := int(time.Since(start).Milliseconds())
	log.Printf("Actuator cycle complete (took %d ms)", totalMs)
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
