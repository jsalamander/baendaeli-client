package colorsensor

import (
	"testing"
)

// fakeReader implements reader for testing without hardware.
type fakeReader struct {
	data []byte
	err  error
}

func (f *fakeReader) Tx(_, r []byte) error {
	if f.err != nil {
		return f.err
	}
	copy(r, f.data)
	return nil
}

func TestReadParsesBytes(t *testing.T) {
	// C=0x0102, R=0x0304, G=0x0506, B=0x0708 (little-endian)
	fake := &fakeReader{data: []byte{0x02, 0x01, 0x04, 0x03, 0x06, 0x05, 0x08, 0x07}}
	s := &Sensor{enabled: true, dev: fake}

	c, r, g, b, err := s.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c != 0x0102 {
		t.Errorf("C: want 0x0102, got 0x%04x", c)
	}
	if r != 0x0304 {
		t.Errorf("R: want 0x0304, got 0x%04x", r)
	}
	if g != 0x0506 {
		t.Errorf("G: want 0x0506, got 0x%04x", g)
	}
	if b != 0x0708 {
		t.Errorf("B: want 0x0708, got 0x%04x", b)
	}
}

func TestReadDisabledReturnsZero(t *testing.T) {
	s := &Sensor{enabled: false}
	c, r, g, b, err := s.Read()
	if err != nil || c != 0 || r != 0 || g != 0 || b != 0 {
		t.Fatalf("expected zeros for disabled sensor, got c=%d r=%d g=%d b=%d err=%v", c, r, g, b, err)
	}
}

func TestSimModeIncrements(t *testing.T) {
	s := &Sensor{enabled: true, sim: true}
	c1, _, _, _, _ := s.Read()
	c2, _, _, _, _ := s.Read()
	if c2 <= c1 {
		t.Errorf("sim mode should increment: c1=%d c2=%d", c1, c2)
	}
}

func TestParseAddr(t *testing.T) {
	cases := []struct {
		in   string
		want uint16
	}{
		{"0x29", 0x29},
		{"0X29", 0x29},
		{"29", 0x29},
		{"0x44", 0x44},
	}
	for _, tc := range cases {
		got, err := parseAddr(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("parseAddr(%q) = %d, %v; want %d", tc.in, got, err, tc.want)
		}
	}
}
