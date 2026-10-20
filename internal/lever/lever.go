package lever

import (
	"errors"
	"fmt"
	"log"

	"github.com/jsalamander/baendaeli-client/internal/config"
	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/host/v3"
)

// Switch reads the two active-low KN3C-223 contacts.
type Switch struct {
	enabled bool
	up      gpio.PinIn
	down    gpio.PinIn
}

func New(cfg *config.Config) *Switch {
	return &Switch{enabled: cfg != nil && cfg.LeverEnabled}
}

func (s *Switch) IsEnabled() bool {
	return s != nil && s.enabled
}

func (s *Switch) Init(cfg *config.Config) error {
	if cfg != nil {
		s.enabled = cfg.LeverEnabled
	}
	if !s.enabled {
		return nil
	}

	upName, downName := "GPIO26", "GPIO21"
	if cfg != nil {
		if cfg.LeverUpPin != "" {
			upName = cfg.LeverUpPin
		}
		if cfg.LeverDownPin != "" {
			downName = cfg.LeverDownPin
		}
	}
	if _, err := host.Init(); err != nil {
		return fmt.Errorf("lever: GPIO initialization failed: %w", err)
	}
	up, down := gpioreg.ByName(upName), gpioreg.ByName(downName)
	if up == nil || down == nil {
		return fmt.Errorf("lever: unavailable GPIO pins UP=%s DOWN=%s", upName, downName)
	}
	if err := s.configurePins(up, down); err != nil {
		return err
	}
	log.Printf("Lever: initialized active-low pull-up inputs UP=%s DOWN=%s", upName, downName)
	return nil
}

func (s *Switch) configurePins(up, down gpio.PinIn) error {
	if up.Name() == down.Name() || up.Number() == down.Number() {
		return fmt.Errorf("lever: UP and DOWN must use different GPIO pins")
	}
	s.up = up
	if err := up.In(gpio.PullUp, gpio.NoEdge); err != nil {
		return errors.Join(
			fmt.Errorf("lever: failed to configure UP pin %s: %w", up.Name(), err),
			s.Close(),
		)
	}
	s.down = down
	if err := down.In(gpio.PullUp, gpio.NoEdge); err != nil {
		return errors.Join(
			fmt.Errorf("lever: failed to configure DOWN pin %s: %w", down.Name(), err),
			s.Close(),
		)
	}
	return nil
}

func (s *Switch) ReadActive() (bool, error) {
	if !s.IsEnabled() {
		return false, nil
	}
	if s.up == nil || s.down == nil {
		return false, fmt.Errorf("lever: GPIO inputs are not initialized")
	}
	return s.up.Read() == gpio.Low || s.down.Read() == gpio.Low, nil
}

func (s *Switch) Close() error {
	var errs []error
	for _, p := range []gpio.PinIn{s.up, s.down} {
		if p != nil {
			if err := p.Halt(); err != nil {
				errs = append(errs, fmt.Errorf("lever: failed to halt pin %s: %w", p.Name(), err))
			}
		}
	}
	s.up, s.down = nil, nil
	return errors.Join(errs...)
}
