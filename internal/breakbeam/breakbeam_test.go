package breakbeam

import (
	"testing"

	"github.com/jsalamander/baendaeli-client/internal/config"
)

func TestNewUsesConfig(t *testing.T) {
	cfg := &config.Config{BreakBeamEnabled: true, BreakBeamPin: "GPIO10"}
	s := New(cfg)

	if !s.IsEnabled() {
		t.Fatal("expected sensor to be enabled")
	}
	if s.pinName != "GPIO10" {
		t.Fatalf("expected pin GPIO10, got %q", s.pinName)
	}
}

func TestReadInterruptedDisabled(t *testing.T) {
	s := &Sensor{}
	triggered, err := s.ReadInterrupted()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if triggered {
		t.Fatal("expected disabled sensor to report not interrupted")
	}
}

func TestReadInterruptedSimulation(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	triggered, err := s.ReadInterrupted()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if triggered {
		t.Fatal("expected simulation sensor to report not interrupted by default")
	}
}
