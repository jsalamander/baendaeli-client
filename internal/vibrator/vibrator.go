package vibrator

import (
	"fmt"
	"log"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/host/v3"
)

// Config holds vibrator hardware configuration.
type Config struct {
	Enabled bool
	IN3Pin  string // e.g., "GPIO16"
	IN4Pin  string // e.g., "GPIO20"
	ENBPin  string // e.g., "GPIO18" (supports hardware PWM on Raspberry Pi)
}

type vibrator struct {
	in3Pin gpio.PinOut
	in4Pin gpio.PinOut
	enbPin gpio.PinOut
	sim    bool
}

var vib *vibrator

// Init initializes the vibrator GPIO pins. Falls back to simulation mode if GPIO is unavailable.
func Init(cfg Config) error {
	if !cfg.Enabled {
		log.Println("Vibrator disabled")
		return nil
	}

	if _, err := host.Init(); err != nil {
		log.Printf("Warning: GPIO not available for vibrator, running in simulation mode: %v", err)
		vib = &vibrator{sim: true}
		return nil
	}

	in3 := gpioreg.ByName(cfg.IN3Pin)
	if in3 == nil {
		log.Printf("Warning: failed to open vibrator IN3 pin %s, running in simulation mode", cfg.IN3Pin)
		vib = &vibrator{sim: true}
		return nil
	}

	in4 := gpioreg.ByName(cfg.IN4Pin)
	if in4 == nil {
		log.Printf("Warning: failed to open vibrator IN4 pin %s, running in simulation mode", cfg.IN4Pin)
		vib = &vibrator{sim: true}
		return nil
	}

	enb := gpioreg.ByName(cfg.ENBPin)
	if enb == nil {
		log.Printf("Warning: failed to open vibrator ENB pin %s, running in simulation mode", cfg.ENBPin)
		vib = &vibrator{sim: true}
		return nil
	}

	// Ensure all pins start LOW
	for _, pin := range []gpio.PinOut{in3, in4, enb} {
		if err := pin.Out(gpio.Low); err != nil {
			return fmt.Errorf("failed to initialise vibrator pin: %w", err)
		}
	}

	vib = &vibrator{
		in3Pin: in3,
		in4Pin: in4,
		enbPin: enb,
	}
	log.Println("Vibrator initialised successfully")
	return nil
}

// Buzz runs the vibrator at the given intensity (0.0–1.0) for the specified duration.
// If the vibrator is not initialised or disabled, this is a no-op.
// Intensity is applied via hardware PWM on the ENB pin; falls back to digital HIGH if
// PWM is not supported by the GPIO driver.
func Buzz(intensity float64, duration time.Duration) error {
	if vib == nil {
		return nil
	}
	if intensity < 0 {
		intensity = 0
	}
	if intensity > 1 {
		intensity = 1
	}

	if vib.sim {
		log.Printf("Vibrator (SIMULATION): buzzing at %.0f%% for %v", intensity*100, duration)
		time.Sleep(duration)
		return nil
	}

	// Set forward direction: IN3=HIGH, IN4=LOW
	if err := vib.in3Pin.Out(gpio.High); err != nil {
		return fmt.Errorf("vibrator: failed to set IN3 high: %w", err)
	}
	if err := vib.in4Pin.Out(gpio.Low); err != nil {
		if stopErr := vib.in3Pin.Out(gpio.Low); stopErr != nil {
			log.Printf("vibrator: failed to reset IN3 after IN4 error: %v", stopErr)
		}
		return fmt.Errorf("vibrator: failed to set IN4 low: %w", err)
	}

	// Attempt hardware PWM on ENB pin; fall back to digital HIGH if unsupported.
	duty := gpio.Duty(float64(gpio.DutyMax) * intensity)
	if err := vib.enbPin.PWM(duty, physic.KiloHertz); err != nil {
		log.Printf("Vibrator: PWM unavailable on ENB pin (falling back to digital HIGH): %v", err)
		if err2 := vib.enbPin.Out(gpio.High); err2 != nil {
			if stopErr := vib.in3Pin.Out(gpio.Low); stopErr != nil {
				log.Printf("vibrator: failed to reset IN3 after ENB error: %v", stopErr)
			}
			return fmt.Errorf("vibrator: failed to set ENB high: %w", err2)
		}
	}

	time.Sleep(duration)

	// Stop: all pins LOW
	if err := vib.in3Pin.Out(gpio.Low); err != nil {
		log.Printf("vibrator: failed to set IN3 low on stop: %v", err)
	}
	if err := vib.enbPin.Out(gpio.Low); err != nil {
		log.Printf("vibrator: failed to set ENB low on stop: %v", err)
	}

	log.Printf("Vibrator: buzzed at %.0f%% for %v", intensity*100, duration)
	return nil
}

// Cleanup safely stops the vibrator and releases GPIO resources.
func Cleanup() {
	if vib == nil {
		return
	}
	if !vib.sim {
		for _, pin := range []gpio.PinOut{vib.in3Pin, vib.in4Pin, vib.enbPin} {
			if pin != nil {
				if err := pin.Out(gpio.Low); err != nil {
					log.Printf("vibrator: cleanup pin.Out error: %v", err)
				}
				if h, ok := pin.(interface{ Halt() error }); ok {
					if err := h.Halt(); err != nil {
						log.Printf("vibrator: cleanup pin.Halt error: %v", err)
					}
				}
			}
		}
	}
	vib = nil
	log.Println("Vibrator cleaned up")
}
