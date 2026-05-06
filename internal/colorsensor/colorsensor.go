package colorsensor

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/jsalamander/baendaeli-client/internal/config"
	"periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/host/v3"
)

// TCS34725 register addresses (command bit 0x80 required)
const (
	cmdBit     = 0x80
	regEnable  = 0x00
	regAtime   = 0x01
	regControl = 0x0F
	regCDATAL  = 0x14 // start of 8-byte block: C, R, G, B (16-bit little-endian each)

	// ENABLE register bits
	ponBit = 0x01 // Power ON
	aenBit = 0x02 // ADC Enable
)

// reader abstracts the I2C device for testing.
type reader interface {
	Tx(w, r []byte) error
}

// Sensor reads RGBA values from a TCS34725 colour sensor via I2C.
type Sensor struct {
	enabled  bool
	sim      bool
	dev      reader
	bus      i2c.BusCloser
	simCount atomic.Uint64
}

// New creates a Sensor from config. Call Init() to open hardware.
func New(cfg *config.Config) *Sensor {
	return &Sensor{enabled: cfg.ColorSensorEnabled}
}

// Init opens the I2C bus and configures the TCS34725.
// Falls back to simulation mode if hardware is unavailable.
func (s *Sensor) Init(cfg *config.Config) error {
	if !s.enabled {
		log.Println("Color sensor disabled")
		return nil
	}

	if _, err := host.Init(); err != nil {
		log.Printf("Color sensor: periph host init failed, running in simulation mode: %v", err)
		s.sim = true
		return nil
	}

	busName := fmt.Sprintf("%d", cfg.ColorSensorI2CBus)
	bus, err := i2creg.Open(busName)
	if err != nil {
		log.Printf("Color sensor: failed to open I2C bus %s, running in simulation mode: %v", busName, err)
		s.sim = true
		return nil
	}

	addr, err := parseAddr(cfg.ColorSensorI2CAddress)
	if err != nil {
		bus.Close()
		return fmt.Errorf("color sensor: invalid I2C address %q: %w", cfg.ColorSensorI2CAddress, err)
	}

	dev := &i2c.Dev{Bus: bus, Addr: addr}

	// Integration time: 0xEB → ~50 ms
	if err := writeReg(dev, regAtime, 0xEB); err != nil {
		bus.Close()
		log.Printf("Color sensor: failed to configure ATIME, running in simulation mode: %v", err)
		s.sim = true
		return nil
	}
	// Gain: 0x01 → 4x
	if err := writeReg(dev, regControl, 0x01); err != nil {
		bus.Close()
		log.Printf("Color sensor: failed to configure CONTROL, running in simulation mode: %v", err)
		s.sim = true
		return nil
	}
	// Power ON, then enable ADC
	if err := writeReg(dev, regEnable, ponBit); err != nil {
		bus.Close()
		log.Printf("Color sensor: failed to power on, running in simulation mode: %v", err)
		s.sim = true
		return nil
	}
	if err := writeReg(dev, regEnable, ponBit|aenBit); err != nil {
		bus.Close()
		log.Printf("Color sensor: failed to enable ADC, running in simulation mode: %v", err)
		s.sim = true
		return nil
	}

	s.bus = bus
	s.dev = dev
	log.Printf("Color sensor initialised on %s addr %#x", busName, addr)
	return nil
}

// Read returns the raw 16-bit C (clear), R, G, B values from the sensor.
func (s *Sensor) Read() (c, r, g, b uint16, err error) {
	if !s.enabled {
		return 0, 0, 0, 0, nil
	}
	if s.sim {
		v := s.simCount.Add(1)
		return uint16(v % 200), 0, 0, 0, nil
	}
	buf := make([]byte, 8)
	if err = s.dev.Tx([]byte{cmdBit | regCDATAL}, buf); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("color sensor read failed: %w", err)
	}
	c = uint16(buf[0]) | uint16(buf[1])<<8
	r = uint16(buf[2]) | uint16(buf[3])<<8
	g = uint16(buf[4]) | uint16(buf[5])<<8
	b = uint16(buf[6]) | uint16(buf[7])<<8
	return c, r, g, b, nil
}

// IsEnabled reports whether the sensor is enabled.
func (s *Sensor) IsEnabled() bool { return s.enabled }

// Close releases the I2C bus.
func (s *Sensor) Close() error {
	if s.bus != nil {
		return s.bus.Close()
	}
	return nil
}

// writeReg writes a single byte to a TCS34725 register.
func writeReg(dev reader, reg, val byte) error {
	return dev.Tx([]byte{cmdBit | reg, val}, nil)
}

// parseAddr parses a hex string like "0x29" or "29" into a uint16.
func parseAddr(s string) (uint16, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	v, err := strconv.ParseUint(s, 16, 16)
	if err != nil {
		return 0, err
	}
	return uint16(v), nil
}
