// +build linux

package main

import (
	"fmt"
	"log"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/host/v3"
)

type ActuatorConfig struct {
	Enabled        bool
	ENAPin         string // e.g., "GPIO25"
	IN1Pin         string // e.g., "GPIO8"
	IN2Pin         string // e.g., "GPIO7"
	ExtendTime     int    // seconds
	RetractTime    int    // seconds
}

type Actuator struct {
	enabled bool
	enaPin  gpio.PinOut
	in1Pin  gpio.PinOut
	in2Pin  gpio.PinOut
	extend  time.Duration
	retract time.Duration
}

var actuator *Actuator

// InitActuator initializes GPIO and the actuator control pins
func InitActuator(config ActuatorConfig) error {
	if !config.Enabled {
		log.Println("Actuator control disabled")
		return nil
	}

	if config.ExtendTime == 0 {
		config.ExtendTime = 20
	}
	if config.RetractTime == 0 {
		config.RetractTime = 20
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
	}

	// Set ENA pin HIGH to enable the actuator
	if err := actuator.enaPin.Out(gpio.High); err != nil {
		return fmt.Errorf("failed to set ENA pin high: %w", err)
	}

	log.Println("Actuator initialized successfully")
	return nil
}

// Trigger executes one extend-retract cycle
func (a *Actuator) Trigger() error {
	if !a.enabled {
		return nil
	}

	log.Println("Actuator: extending...")
	// Extend: IN1 HIGH, IN2 LOW
	if err := a.in1Pin.Out(gpio.High); err != nil {
		return fmt.Errorf("failed to set IN1 high: %w", err)
	}
	if err := a.in2Pin.Out(gpio.Low); err != nil {
		return fmt.Errorf("failed to set IN2 low: %w", err)
	}

	time.Sleep(a.extend)

	log.Println("Actuator: retracting...")
	// Retract: IN1 LOW, IN2 HIGH
	if err := a.in1Pin.Out(gpio.Low); err != nil {
		return fmt.Errorf("failed to set IN1 low: %w", err)
	}
	if err := a.in2Pin.Out(gpio.High); err != nil {
		return fmt.Errorf("failed to set IN2 high: %w", err)
	}

	time.Sleep(a.retract)

	// Stop: both LOW
	if err := a.in1Pin.Out(gpio.Low); err != nil {
		return fmt.Errorf("failed to set IN1 low: %w", err)
	}
	if err := a.in2Pin.Out(gpio.Low); err != nil {
		return fmt.Errorf("failed to set IN2 low: %w", err)
	}

	log.Println("Actuator: cycle complete")
	return nil
}

// TriggerActuator is the public entry point to trigger the actuator
func TriggerActuator() error {
	if actuator == nil || !actuator.enabled {
		return nil
	}
	return actuator.Trigger()
}

// Cleanup closes GPIO resources
func CleanupActuator() {
	if actuator != nil {
		actuator.enaPin.Out(gpio.Low)
		actuator.enaPin.Halt()
		actuator.in1Pin.Halt()
		actuator.in2Pin.Halt()
		log.Println("Actuator GPIO cleaned up")
	}
}
