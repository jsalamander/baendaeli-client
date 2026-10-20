package lever

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/jsalamander/baendaeli-client/internal/config"
	"periph.io/x/conn/v3/gpio"
)

func TestSwitchLogsInitialContactsAndChangesOnly(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)

	up := &stubPin{name: "GPIO26", number: 26, level: gpio.High}
	down := &stubPin{name: "GPIO21", number: 21, level: gpio.High}
	s := New(&config.Config{LeverEnabled: true})
	if err := s.configurePins(up, down); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := s.ReadActive(); err != nil {
			t.Fatal(err)
		}
	}
	for _, levels := range [][2]gpio.Level{
		{gpio.Low, gpio.High},
		{gpio.High, gpio.Low},
		{gpio.High, gpio.High},
	} {
		up.level, down.level = levels[0], levels[1]
		if _, err := s.ReadActive(); err != nil {
			t.Fatal(err)
		}
	}
	text := output.String()
	if strings.Count(text, "Lever: contacts") != 4 {
		t.Fatalf("expected initial reading plus three changes, not a log per poll: %s", text)
	}
	for _, expected := range []string{"UP=High DOWN=High", "UP=Low DOWN=High", "UP=High DOWN=Low"} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q in contact logs: %s", expected, text)
		}
	}
}

type stubPin struct {
	gpio.PinIn
	name    string
	number  int
	level   gpio.Level
	pull    gpio.Pull
	edge    gpio.Edge
	inErr   error
	haltErr error
	halted  bool
}

func (p *stubPin) Name() string { return p.name }
func (p *stubPin) Number() int  { return p.number }
func (p *stubPin) In(pull gpio.Pull, edge gpio.Edge) error {
	p.pull, p.edge = pull, edge
	return p.inErr
}
func (p *stubPin) Read() gpio.Level { return p.level }
func (p *stubPin) Halt() error {
	p.halted = true
	return p.haltErr
}

func TestSwitchDisabled(t *testing.T) {
	for _, cfg := range []*config.Config{nil, {}} {
		s := New(cfg)
		if s.IsEnabled() {
			t.Fatal("lever must be disabled by default")
		}
		if err := s.Init(cfg); err != nil {
			t.Fatalf("disabled Init: %v", err)
		}
		active, err := s.ReadActive()
		if err != nil || active {
			t.Fatalf("disabled ReadActive = %t, %v", active, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSwitchActiveLowInputs(t *testing.T) {
	up := &stubPin{name: "GPIO26", number: 26}
	down := &stubPin{name: "GPIO21", number: 21}
	s := New(&config.Config{LeverEnabled: true})
	if err := s.configurePins(up, down); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*stubPin{up, down} {
		if p.pull != gpio.PullUp || p.edge != gpio.NoEdge {
			t.Fatalf("%s must use pull-up input without edge interrupts", p.name)
		}
	}
	for _, tc := range []struct {
		name   string
		up     gpio.Level
		down   gpio.Level
		active bool
	}{
		{"neutral", gpio.High, gpio.High, false},
		{"up", gpio.Low, gpio.High, true},
		{"down", gpio.High, gpio.Low, true},
		{"both", gpio.Low, gpio.Low, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up.level, down.level = tc.up, tc.down
			active, err := s.ReadActive()
			if err != nil || active != tc.active {
				t.Fatalf("ReadActive = %t, %v; want %t", active, err, tc.active)
			}
		})
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if !up.halted || !down.halted {
		t.Fatal("both GPIO inputs must be halted")
	}
	if _, err := s.ReadActive(); err == nil {
		t.Fatal("enabled but closed input must report an error")
	}
}

func TestSwitchConfigurationErrors(t *testing.T) {
	t.Run("duplicate pins", func(t *testing.T) {
		s := New(&config.Config{LeverEnabled: true})
		if err := s.configurePins(&stubPin{name: "GPIO26", number: 26}, &stubPin{name: "alias26", number: 26}); err == nil {
			t.Fatal("expected duplicate GPIO error")
		}
	})
	for _, contact := range []string{"up", "down"} {
		t.Run(contact, func(t *testing.T) {
			up := &stubPin{name: "GPIO26", number: 26}
			down := &stubPin{name: "GPIO21", number: 21}
			want := errors.New("input configuration failed")
			if contact == "up" {
				up.inErr = want
			} else {
				down.inErr = want
			}
			s := New(&config.Config{LeverEnabled: true})
			if err := s.configurePins(up, down); !errors.Is(err, want) {
				t.Fatalf("expected configuration failure, got %v", err)
			}
			if contact == "down" && !up.halted {
				t.Fatal("UP input must be cleaned up if DOWN configuration fails")
			}
			if _, err := s.ReadActive(); err == nil {
				t.Fatal("partially initialized switch must not read successfully")
			}
		})
	}
}

func TestSwitchCloseReportsBothErrors(t *testing.T) {
	upErr, downErr := errors.New("UP halt failed"), errors.New("DOWN halt failed")
	s := &Switch{
		up:   &stubPin{name: "GPIO26", haltErr: upErr},
		down: &stubPin{name: "GPIO21", haltErr: downErr},
	}
	err := s.Close()
	if !errors.Is(err, upErr) || !errors.Is(err, downErr) {
		t.Fatalf("expected both cleanup errors, got %v", err)
	}
}
