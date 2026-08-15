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

func TestPrinterTextUsesSingleByteUmlaut(t *testing.T) {
	got := printerText("Solibändeli")
	want := []byte{'S', 'o', 'l', 'i', 'b', 0x84, 'n', 'd', 'e', 'l', 'i'}

	if string(got) != string(want) {
		t.Errorf("printerText() = %v, want %v", got, want)
	}
}

func TestInitDisabledPrinterDoesNotPrint(t *testing.T) {
	disabled := false
	cfg := &config.Config{PrinterEnabled: &disabled, PrinterDevicePath: "/dev/does-not-exist"}

	p := New(cfg)
	if err := p.Init(cfg); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if p.IsEnabled() {
		t.Error("printer should be disabled")
	}
	if err := p.PrintPaymentTicket(2000, 1); err != nil {
		t.Errorf("disabled PrintPaymentTicket returned error: %v", err)
	}
}

func TestPrinterEnabledByDefault(t *testing.T) {
	cfg := &config.Config{PrinterDevicePath: "/dev/does-not-exist"}

	p := New(cfg)
	if !p.IsEnabled() {
		t.Fatal("printer should be enabled when PRINTER_ENABLED is absent")
	}
}

func TestInitSimulationFallbackWhenDeviceMissing(t *testing.T) {
	cfg := &config.Config{PrinterDevicePath: "/dev/does-not-exist"}

	p := New(cfg)
	if err := p.Init(cfg); err != nil {
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
