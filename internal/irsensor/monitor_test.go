package irsensor

import (
	"sync"
	"testing"
	"time"

	"periph.io/x/conn/v3/gpio"
)

type fakeEdgePin struct {
	mu     sync.Mutex
	level  gpio.Level
	edges  chan gpio.Level
	halted bool
}

func newFakeEdgePin() *fakeEdgePin {
	return &fakeEdgePin{
		level: gpio.High,
		edges: make(chan gpio.Level, 16),
	}
}

func (p *fakeEdgePin) Emit(level gpio.Level) {
	p.edges <- level
}

func (p *fakeEdgePin) WaitForEdge(timeout time.Duration) bool {
	select {
	case level := <-p.edges:
		p.mu.Lock()
		p.level = level
		p.mu.Unlock()
		return true
	case <-time.After(timeout):
		return false
	}
}

func (p *fakeEdgePin) Read() gpio.Level {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.level
}

func (p *fakeEdgePin) Halt() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.halted = true
	return nil
}

func (p *fakeEdgePin) IsHalted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.halted
}

func TestMeasureDisabledRunsAction(t *testing.T) {
	m := &Monitor{}
	called := false

	count, err := m.Measure(func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !called {
		t.Fatal("expected action to be called")
	}
	if count != 0 {
		t.Fatalf("expected count=0, got %d", count)
	}
}

func TestMeasureCountsAcrossPins(t *testing.T) {
	pin1 := newFakeEdgePin()
	pin2 := newFakeEdgePin()
	m := &Monitor{
		enabled:  true,
		debounce: 0,
		pins:     []edgePin{pin1, pin2},
	}

	count, err := m.Measure(func() error {
		pin1.Emit(gpio.Low)
		pin2.Emit(gpio.Low)
		pin1.Emit(gpio.Low)
		time.Sleep(30 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if count != 3 {
		t.Fatalf("expected count=3, got %d", count)
	}
}

func TestMeasureDebouncesPerPin(t *testing.T) {
	pin := newFakeEdgePin()
	m := &Monitor{
		enabled:  true,
		debounce: 30 * time.Millisecond,
		pins:     []edgePin{pin},
	}

	count, err := m.Measure(func() error {
		pin.Emit(gpio.Low)
		time.Sleep(5 * time.Millisecond)
		pin.Emit(gpio.Low)
		time.Sleep(40 * time.Millisecond)
		pin.Emit(gpio.Low)
		time.Sleep(30 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if count != 2 {
		t.Fatalf("expected count=2 with debounce, got %d", count)
	}
}

func TestCloseHaltsPins(t *testing.T) {
	pin1 := newFakeEdgePin()
	pin2 := newFakeEdgePin()
	m := &Monitor{
		enabled: true,
		pins:    []edgePin{pin1, pin2},
	}

	if err := m.Close(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !pin1.IsHalted() || !pin2.IsHalted() {
		t.Fatal("expected all pins to be halted")
	}
}
