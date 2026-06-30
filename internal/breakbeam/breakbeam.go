package breakbeam

import (
	"fmt"
	"log"

	"github.com/jsalamander/baendaeli-client/internal/config"
	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/host/v3"
)

// Sensor provides read access to the IR break-beam receiver.
// With pull-up wiring, a LOW input means the beam is interrupted.
type Sensor struct {
	enabled bool
	pinName string
	pin     gpio.PinIn
	sim     bool
}

func New(cfg *config.Config) *Sensor {
	if cfg == nil {
		return &Sensor{}
	}
	return &Sensor{
		enabled: cfg.BreakBeamEnabled,
		pinName: cfg.BreakBeamPin,
	}
}

func (s *Sensor) IsEnabled() bool {
	return s != nil && s.enabled
}

func (s *Sensor) IsSimulation() bool {
	return s != nil && s.sim
}

func (s *Sensor) Init(cfg *config.Config) error {
	if s == nil {
		return nil
	}
	if cfg != nil {
		s.enabled = cfg.BreakBeamEnabled
		if cfg.BreakBeamPin != "" {
			s.pinName = cfg.BreakBeamPin
		}
	}
	if !s.enabled {
		return nil
	}
	if s.pinName == "" {
		s.pinName = "GPIO10"
	}

	if _, err := host.Init(); err != nil {
		log.Printf("Break-beam: GPIO unavailable, running in simulation mode: %v", err)
		s.sim = true
		return nil
	}

	pin := gpioreg.ByName(s.pinName)
	if pin == nil {
		log.Printf("Break-beam: failed to open pin %s, running in simulation mode", s.pinName)
		s.sim = true
		return nil
	}

	if err := pin.In(gpio.PullUp, gpio.NoEdge); err != nil {
		return fmt.Errorf("break-beam: failed to configure pin %s: %w", s.pinName, err)
	}

	s.pin = pin
	s.sim = false
	log.Printf("Break-beam: initialized on %s", s.pinName)
	return nil
}

// ReadInterrupted returns true when the beam is blocked.
// For the configured pull-up input, LOW means interrupted.
func (s *Sensor) ReadInterrupted() (bool, error) {
	if s == nil || !s.enabled {
		return false, nil
	}
	if s.sim || s.pin == nil {
		return false, nil
	}
	return s.pin.Read() == gpio.Low, nil
}

func (s *Sensor) Close() error {
	if s == nil || s.pin == nil {
		return nil
	}
	if h, ok := s.pin.(interface{ Halt() error }); ok {
		if err := h.Halt(); err != nil {
			return fmt.Errorf("break-beam: failed to halt pin %s: %w", s.pinName, err)
		}
	}
	s.pin = nil
	return nil
}
