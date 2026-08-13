package printer

import (
	"testing"

	"github.com/jsalamander/baendaeli-client/internal/config"
)

func TestQuantityLine(t *testing.T) {
	cases := []struct {
		count int
		want  string
	}{
		{count: 0, want: "0x Solibändeli ERROR"},
		{count: -1, want: "0x Solibändeli ERROR"},
		{count: 1, want: "1x Solibändeli"},
		{count: 3, want: "1x Solibändeli"},
	}

	for _, tc := range cases {
		if got := quantityLine(tc.count); got != tc.want {
			t.Errorf("quantityLine(%d) = %q, want %q", tc.count, got, tc.want)
		}
	}
}

func TestFormatAmount(t *testing.T) {
	cases := []struct {
		cents int64
		want  string
	}{
		{cents: 2000, want: "20.00 CHF"},
		{cents: 150, want: "1.50 CHF"},
		{cents: 0, want: "0.00 CHF"},
	}

	for _, tc := range cases {
		if got := formatAmount(tc.cents); got != tc.want {
			t.Errorf("formatAmount(%d) = %q, want %q", tc.cents, got, tc.want)
		}
	}
}

func TestInitSimulationFallbackWhenDisabled(t *testing.T) {
	p := New(&config.Config{PrinterEnabled: false, PrinterDevicePath: "/dev/does-not-exist"})
	if err := p.Init(&config.Config{PrinterEnabled: false, PrinterDevicePath: "/dev/does-not-exist"}); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if p.IsSimulation() {
		t.Error("disabled printer should not be marked as simulation")
	}
	if p.IsEnabled() {
		t.Error("printer should be disabled")
	}
}

func TestInitSimulationFallbackWhenDeviceMissing(t *testing.T) {
	p := New(&config.Config{PrinterEnabled: true, PrinterDevicePath: "/dev/does-not-exist"})
	if err := p.Init(&config.Config{PrinterEnabled: true, PrinterDevicePath: "/dev/does-not-exist"}); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if !p.IsSimulation() {
		t.Error("expected simulation mode when device path is missing")
	}

	// Simulated printer must no-op instead of failing.
	if err := p.PrintPaymentTicket(2000, 1); err != nil {
		t.Errorf("PrintPaymentTicket in simulation mode returned error: %v", err)
	}
	if err := p.PrintStartupMessage(); err != nil {
		t.Errorf("PrintStartupMessage in simulation mode returned error: %v", err)
	}
}

func TestNewWithNilConfig(t *testing.T) {
	p := New(nil)
	if p.IsEnabled() {
		t.Error("printer built from nil config should be disabled")
	}
}
