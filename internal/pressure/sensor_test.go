package pressure

import (
	"errors"
	"testing"
)

// stubBus is a test double for i2cBus.
type stubBus struct {
	txErr      error
	rxBuf      []byte // bytes to return on read
	txCalls    [][]byte
	closeCalled bool
	closeErr   error
}

func (b *stubBus) Tx(addr uint16, w, r []byte) error {
	if b.txErr != nil {
		return b.txErr
	}
	b.txCalls = append(b.txCalls, append([]byte{}, w...))
	if len(r) > 0 && len(b.rxBuf) >= len(r) {
		copy(r, b.rxBuf[:len(r)])
	}
	return nil
}

func (b *stubBus) Close() error {
	b.closeCalled = true
	return b.closeErr
}

func newSensor(threshold float64, rxBuf []byte) *sensor {
	return &sensor{
		bus:       &stubBus{rxBuf: rxBuf},
		addr:      0x48,
		threshold: threshold,
	}
}

// rawToBytes converts a signed int16 raw ADC value to big-endian bytes.
func rawToBytes(raw int16) []byte {
	u := uint16(raw)
	return []byte{byte(u >> 8), byte(u & 0xFF)}
}

func TestIsBallLoaded_AboveThreshold(t *testing.T) {
	// Raw value 16000 → 16000 * 0.000125 = 2.0 V; threshold = 1.0 V → loaded
	raw := int16(16000)
	s := newSensor(1.0, rawToBytes(raw))
	loaded, err := s.isBallLoaded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !loaded {
		t.Fatal("expected ball to be detected (voltage above threshold)")
	}
}

func TestIsBallLoaded_BelowThreshold(t *testing.T) {
	// Raw value 4000 → 4000 * 0.000125 = 0.5 V; threshold = 1.0 V → not loaded
	raw := int16(4000)
	s := newSensor(1.0, rawToBytes(raw))
	loaded, err := s.isBallLoaded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded {
		t.Fatal("expected ball not detected (voltage below threshold)")
	}
}

func TestIsBallLoaded_AtThreshold(t *testing.T) {
	// Raw value 8000 → 8000 * 0.000125 = 1.0 V; threshold = 1.0 V → loaded (>=)
	raw := int16(8000)
	s := newSensor(1.0, rawToBytes(raw))
	loaded, err := s.isBallLoaded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !loaded {
		t.Fatal("expected ball detected at exact threshold")
	}
}

func TestIsBallLoaded_NegativeRaw(t *testing.T) {
	// Negative ADC value → very small or negative voltage → not loaded
	raw := int16(-100)
	s := newSensor(1.0, rawToBytes(raw))
	loaded, err := s.isBallLoaded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded {
		t.Fatal("expected ball not detected (negative voltage)")
	}
}

func TestIsBallLoaded_I2CError(t *testing.T) {
	s := &sensor{
		bus:       &stubBus{txErr: errors.New("i2c bus error")},
		addr:      0x48,
		threshold: 1.0,
	}
	_, err := s.isBallLoaded()
	if err == nil {
		t.Fatal("expected error on I2C failure")
	}
}

func TestIsBallLoaded_Simulation(t *testing.T) {
	s := &sensor{sim: true, threshold: 1.0}
	loaded, err := s.isBallLoaded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !loaded {
		t.Fatal("simulation mode should always report ball loaded")
	}
}

func TestIsBallLoaded_GlobalNil(t *testing.T) {
	// Save and restore global sensor
	prev := globalSensor
	globalSensor = nil
	defer func() { globalSensor = prev }()

	loaded, err := IsBallLoaded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !loaded {
		t.Fatal("nil sensor should report ball loaded (safe default)")
	}
}

func TestCleanup_ClosesI2CBus(t *testing.T) {
	stub := &stubBus{}
	prev := globalSensor
	globalSensor = &sensor{bus: stub, addr: 0x48, threshold: 1.0}
	defer func() { globalSensor = prev }()

	Cleanup()

	if !stub.closeCalled {
		t.Fatal("expected I2C bus Close() to be called on Cleanup")
	}
	if globalSensor != nil {
		t.Fatal("expected globalSensor to be nil after Cleanup")
	}
}

func TestCleanup_SimulationMode(t *testing.T) {
	prev := globalSensor
	globalSensor = &sensor{sim: true, threshold: 1.0}
	defer func() { globalSensor = prev }()

	// Should not panic or call Close on nil bus
	Cleanup()
	if globalSensor != nil {
		t.Fatal("expected globalSensor to be nil after Cleanup in simulation mode")
	}
}

func TestReadVolts_Simulation(t *testing.T) {
	prev := globalSensor
	globalSensor = &sensor{sim: true, threshold: 1.0}
	defer func() { globalSensor = prev }()

	v, err := ReadVolts()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v <= 0 {
		t.Fatalf("expected positive voltage in simulation mode, got %f", v)
	}
}

func TestReadVolts_NilSensor(t *testing.T) {
	prev := globalSensor
	globalSensor = nil
	defer func() { globalSensor = prev }()

	v, err := ReadVolts()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 0 {
		t.Fatalf("expected 0 volts for nil sensor, got %f", v)
	}
}

func TestReadRaw_WritesCorrectConfig(t *testing.T) {
	stub := &stubBus{rxBuf: []byte{0x3E, 0x80}} // 0x3E80 = 16000
	s := &sensor{bus: stub, addr: 0x48, threshold: 1.0}

	raw, err := s.readRaw()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw != 16000 {
		t.Fatalf("expected raw=16000, got %d", raw)
	}

	// First call writes config, second reads conversion
	if len(stub.txCalls) < 2 {
		t.Fatalf("expected at least 2 TX calls, got %d", len(stub.txCalls))
	}
	// First write: [regConfig, highByte, lowByte]
	if stub.txCalls[0][0] != regConfig {
		t.Fatalf("first write should be to regConfig (0x%02X), got 0x%02X", regConfig, stub.txCalls[0][0])
	}
	// Second write: [regConversion] (pointer set before read)
	if stub.txCalls[1][0] != regConversion {
		t.Fatalf("second write should be to regConversion (0x%02X), got 0x%02X", regConversion, stub.txCalls[1][0])
	}
}
