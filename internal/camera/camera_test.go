package camera

import (
	"encoding/base64"
	"testing"
)

func TestInitDisabled(t *testing.T) {
	err := Init(Config{Enabled: false})
	if err != nil {
		t.Fatalf("expected no error when disabled, got %v", err)
	}
	if c != nil {
		t.Error("expected singleton to stay nil when disabled")
	}
}

func TestInitSimulationMode(t *testing.T) {
	// Force simulation mode by calling Init with Enabled: true on a host without libcamera.
	// Init falls back to sim when neither libcamera-still nor rpicam-still is found in PATH.
	err := Init(Config{Enabled: true})
	if err != nil {
		t.Fatalf("Init returned unexpected error: %v", err)
	}
	defer Cleanup()

	if c == nil {
		t.Fatal("expected singleton to be set after Init")
	}
}

func TestCaptureSimulationReturnsValidJPEG(t *testing.T) {
	// Directly set the singleton to sim mode to isolate the test from PATH.
	c = &cam{sim: true}
	defer func() { c = nil }()

	data, err := Capture()
	if err != nil {
		t.Fatalf("Capture returned unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty image data")
	}
	// Verify JPEG magic bytes (SOI marker 0xFFD8)
	if data[0] != 0xff || data[1] != 0xd8 {
		t.Errorf("expected JPEG SOI marker 0xFFD8, got 0x%02x%02x", data[0], data[1])
	}
	// Verify JPEG EOI marker (0xFFD9) at the end
	n := len(data)
	if data[n-2] != 0xff || data[n-1] != 0xd9 {
		t.Errorf("expected JPEG EOI marker 0xFFD9 at end, got 0x%02x%02x", data[n-2], data[n-1])
	}
}

func TestCaptureSimulationBase64Roundtrip(t *testing.T) {
	c = &cam{sim: true}
	defer func() { c = nil }()

	data, err := Capture()
	if err != nil {
		t.Fatalf("Capture returned unexpected error: %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	if len(encoded) == 0 {
		t.Fatal("expected non-empty base64 string")
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}
	if len(decoded) != len(data) {
		t.Errorf("expected decoded length %d, got %d", len(data), len(decoded))
	}
}

func TestCaptureWhenNotInitialised(t *testing.T) {
	c = nil

	_, err := Capture()
	if err == nil {
		t.Fatal("expected error when camera not initialised, got nil")
	}
}

func TestCleanup(t *testing.T) {
	c = &cam{sim: true}
	Cleanup()
	if c != nil {
		t.Error("expected singleton to be nil after Cleanup")
	}
}

func TestSimulatedJPEGSize(t *testing.T) {
	data := simulatedJPEG()
	if len(data) == 0 {
		t.Fatal("simulatedJPEG must not be empty")
	}
	if len(data) > maxJPEGBytes {
		t.Errorf("simulated JPEG exceeds max size: %d > %d", len(data), maxJPEGBytes)
	}
}
