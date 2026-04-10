package pressure

import (
	"fmt"
	"log"
	"time"

	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/host/v3"
)

// ADS1115 register addresses
const (
	regConversion = 0x00
	regConfig     = 0x01
)

// ADS1115 config register bit masks (single-shot A0 vs GND, +/-4.096V, 128SPS)
const (
	osSingle    = 0x8000 // Start single conversion
	muxA0GND    = 0x4000 // A0 single-ended vs GND
	pga4096V    = 0x0200 // +/-4.096V (LSB = 125uV)
	modeSingle  = 0x0100 // Single-shot mode
	dr128sps    = 0x0080 // 128 samples per second
	compDisable = 0x0003 // Disable comparator

	ads1115Config = osSingle | muxA0GND | pga4096V | modeSingle | dr128sps | compDisable

	// conversionDelay is the safe wait time for a single-shot conversion at 128SPS
	conversionDelay = 10 * time.Millisecond

	// voltsPerLSB for PGA +/-4.096V: 4.096/32768 = 0.000125 V
	voltsPerLSB = 0.000125
)

// Config holds pressure sensor hardware configuration.
type Config struct {
	Enabled         bool
	I2CBus          string  // e.g., "1" for /dev/i2c-1
	I2CAddr         uint16  // ADS1115 I2C address, default 0x48
	ThresholdVolts  float64 // Minimum voltage to consider a ball present
	MaxVibraCycles  int     // Maximum vibration cycles before declaring stau
	VibraDurationMs int     // Duration of each vibration pulse in milliseconds
}

// i2cBus is an interface for I2C bus operations, used for testability.
type i2cBus interface {
	Tx(addr uint16, w, r []byte) error
	Close() error
}

type sensor struct {
	sim       bool
	bus       i2cBus
	addr      uint16
	threshold float64
}

var globalSensor *sensor

// Init initialises the pressure sensor. Falls back to simulation mode if I2C is unavailable.
func Init(cfg Config) error {
	if !cfg.Enabled {
		log.Println("Pressure sensor disabled")
		return nil
	}

	if cfg.I2CAddr == 0 {
		cfg.I2CAddr = 0x48
	}
	if cfg.I2CBus == "" {
		cfg.I2CBus = "1"
	}

	if _, err := host.Init(); err != nil {
		log.Printf("Warning: host init failed for pressure sensor, running in simulation mode: %v", err)
		globalSensor = &sensor{sim: true, threshold: cfg.ThresholdVolts}
		return nil
	}

	bus, err := i2creg.Open(cfg.I2CBus)
	if err != nil {
		log.Printf("Warning: failed to open I2C bus %s for pressure sensor, running in simulation mode: %v", cfg.I2CBus, err)
		globalSensor = &sensor{sim: true, threshold: cfg.ThresholdVolts}
		return nil
	}

	globalSensor = &sensor{
		bus:       bus,
		addr:      cfg.I2CAddr,
		threshold: cfg.ThresholdVolts,
	}
	log.Printf("Pressure sensor (ADS1115) initialised on I2C bus %s, addr=0x%02X, threshold=%.4fV",
		cfg.I2CBus, cfg.I2CAddr, cfg.ThresholdVolts)
	return nil
}

// IsBallLoaded reads the ADS1115 and returns true if the measured voltage
// is at or above the configured threshold, indicating a ball is present in the holder.
func IsBallLoaded() (bool, error) {
	if globalSensor == nil {
		// Sensor not configured: assume ball is loaded so the flow continues normally.
		return true, nil
	}
	return globalSensor.isBallLoaded()
}

func (s *sensor) isBallLoaded() (bool, error) {
	if s.sim {
		log.Println("Pressure sensor (SIMULATION): ball detected")
		return true, nil
	}

	v, err := s.readVolts()
	if err != nil {
		return false, fmt.Errorf("pressure sensor: read failed: %w", err)
	}

	loaded := v >= s.threshold
	log.Printf("Pressure sensor: %.4fV (threshold=%.4fV, loaded=%v)", v, s.threshold, loaded)
	return loaded, nil
}

// ReadVolts performs a single-shot ADC measurement and returns the voltage on A0.
func ReadVolts() (float64, error) {
	if globalSensor == nil {
		return 0, nil
	}
	if globalSensor.sim {
		log.Println("Pressure sensor (SIMULATION): reading volts → 2.5V")
		return 2.5, nil
	}
	return globalSensor.readVolts()
}

func (s *sensor) readVolts() (float64, error) {
	raw, err := s.readRaw()
	if err != nil {
		return 0, err
	}
	return float64(raw) * voltsPerLSB, nil
}

func (s *sensor) readRaw() (int16, error) {
	// Write 16-bit big-endian config to trigger single-shot conversion
	high := byte((ads1115Config >> 8) & 0xFF)
	low := byte(ads1115Config & 0xFF)
	if err := s.bus.Tx(s.addr, []byte{regConfig, high, low}, nil); err != nil {
		return 0, fmt.Errorf("failed to write ADS1115 config: %w", err)
	}

	// Wait for conversion to complete (~8ms at 128SPS, 10ms is safe)
	time.Sleep(conversionDelay)

	// Read 2 bytes from the conversion register
	buf := make([]byte, 2)
	if err := s.bus.Tx(s.addr, []byte{regConversion}, buf); err != nil {
		return 0, fmt.Errorf("failed to read ADS1115 conversion: %w", err)
	}

	raw := (int16(buf[0]) << 8) | int16(buf[1])
	return raw, nil
}

// Cleanup closes the I2C bus and releases resources.
func Cleanup() {
	if globalSensor == nil {
		return
	}
	if !globalSensor.sim && globalSensor.bus != nil {
		if err := globalSensor.bus.Close(); err != nil {
			log.Printf("Pressure sensor: failed to close I2C bus: %v", err)
		}
	}
	globalSensor = nil
	log.Println("Pressure sensor cleaned up")
}
