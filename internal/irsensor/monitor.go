package irsensor

import (
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jsalamander/baendaeli-client/internal/config"
	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/host/v3"
)

const edgeWaitTimeout = 50 * time.Millisecond

type Monitor struct {
	enabled  bool
	debounce time.Duration
	pins     []edgePin
}

type edgePin interface {
	WaitForEdge(time.Duration) bool
	Read() gpio.Level
	Halt() error
}

func New(cfg *config.Config) *Monitor {
	if cfg == nil || !cfg.IRSensorEnabled {
		return &Monitor{}
	}

	if _, err := host.Init(); err != nil {
		log.Printf("IR sensor: GPIO unavailable, disabling monitoring: %v", err)
		return &Monitor{}
	}

	pinNames := []string{cfg.IRSensor1Pin, cfg.IRSensor2Pin}
	pins := make([]edgePin, 0, len(pinNames))
	for _, name := range pinNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		pin := gpioreg.ByName(name)
		if pin == nil {
			log.Printf("IR sensor: pin %s not available, disabling monitoring", name)
			return &Monitor{}
		}
		if err := pin.In(gpio.PullUp, gpio.FallingEdge); err != nil {
			log.Printf("IR sensor: failed to configure pin %s, disabling monitoring: %v", name, err)
			return &Monitor{}
		}

		pins = append(pins, pin)
	}

	if len(pins) == 0 {
		log.Printf("IR sensor: enabled but no pins configured, disabling monitoring")
		return &Monitor{}
	}

	log.Printf("IR sensor: monitoring enabled on %d pin(s)", len(pins))
	return &Monitor{
		enabled:  true,
		debounce: time.Duration(cfg.IRSensorDebounceMs) * time.Millisecond,
		pins:     pins,
	}
}

func (m *Monitor) Measure(action func() error) (int, error) {
	if !m.enabled || len(m.pins) == 0 {
		return 0, action()
	}

	var count atomic.Int64
	stopCh := make(chan struct{})
	var wg sync.WaitGroup

	for index, pin := range m.pins {
		wg.Add(1)
		go func(sensorIndex int, pin edgePin) {
			defer wg.Done()

			var lastTrigger time.Time
			for {
				select {
				case <-stopCh:
					return
				default:
				}

				if !pin.WaitForEdge(edgeWaitTimeout) {
					continue
				}

				now := time.Now()
				if !lastTrigger.IsZero() && now.Sub(lastTrigger) < m.debounce {
					continue
				}
				if pin.Read() != gpio.Low {
					continue
				}

				lastTrigger = now
				total := count.Add(1)
				log.Printf("IR sensor %d detected beam break, count=%d", sensorIndex, total)
			}
		}(index+1, pin)
	}

	err := action()
	close(stopCh)
	wg.Wait()

	return int(count.Load()), err
}

func (m *Monitor) Close() error {
	for _, pin := range m.pins {
		if pin == nil {
			continue
		}
		if err := pin.Halt(); err != nil {
			return err
		}
	}
	return nil
}
