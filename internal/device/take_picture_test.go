package device

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jsalamander/baendaeli-client/internal/camera"
	"github.com/jsalamander/baendaeli-client/internal/config"
)

// initSimCamera sets the camera singleton to simulation mode and returns a cleanup func.
func initSimCamera(t *testing.T) func() {
	t.Helper()
	if err := camera.Init(camera.Config{Enabled: true}); err != nil {
		t.Fatalf("camera.Init failed: %v", err)
	}
	return camera.Cleanup
}

func TestTakePictureCommandSuccess(t *testing.T) {
	defer initSimCamera(t)()

	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	cmd := &CommandResponse{
		ID:      99,
		Command: "take_picture",
	}

	imageData, err := client.executeCommand(cmd)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if imageData == "" {
		t.Fatal("expected non-empty base64 image data")
	}

	// Verify the returned value is valid base64
	decoded, err := base64.StdEncoding.DecodeString(imageData)
	if err != nil {
		t.Fatalf("returned image_data is not valid base64: %v", err)
	}
	// Verify it looks like a JPEG (SOI marker)
	if len(decoded) < 2 || decoded[0] != 0xff || decoded[1] != 0xd8 {
		t.Errorf("expected JPEG SOI marker, got %x", decoded[:2])
	}
}

func TestTakePictureCommandCameraNotInitialised(t *testing.T) {
	// Ensure camera is not initialised
	camera.Cleanup()

	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	cmd := &CommandResponse{
		ID:      100,
		Command: "take_picture",
	}

	imageData, err := client.executeCommand(cmd)
	if err == nil {
		t.Fatal("expected error when camera not initialised, got nil")
	}
	if imageData != "" {
		t.Errorf("expected empty image data on failure, got %q", imageData)
	}
}

func TestAckRequestIncludesImageBase64OnSuccess(t *testing.T) {
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/ack") {
			var buf [4096]byte
			n, _ := r.Body.Read(buf[:])
			capturedBody = buf[:n]
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success": true}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaendaeliURL:    server.URL,
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	fakeImage := base64.StdEncoding.EncodeToString([]byte("fake-jpeg-bytes"))
	err := client.ackCommand(99, nil, fakeImage)
	if err != nil {
		t.Fatalf("ackCommand returned unexpected error: %v", err)
	}

	var req AckRequest
	if err := json.Unmarshal(capturedBody, &req); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}
	if req.Status != "success" {
		t.Errorf("expected status 'success', got %q", req.Status)
	}
	if req.ImageBase64 != fakeImage {
		t.Errorf("expected image_base64 %q, got %q", fakeImage, req.ImageBase64)
	}
	if req.ErrorMessage != "" {
		t.Errorf("expected no error_message on success, got %q", req.ErrorMessage)
	}
}

func TestAckRequestOmitsImageBase64OnFailure(t *testing.T) {
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/ack") {
			var buf [4096]byte
			n, _ := r.Body.Read(buf[:])
			capturedBody = buf[:n]
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success": true}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaendaeliURL:    server.URL,
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	execErr := fmt.Errorf("camera unavailable")
	err := client.ackCommand(100, execErr, "")
	if err != nil {
		t.Fatalf("ackCommand returned unexpected error: %v", err)
	}

	var req AckRequest
	if err := json.Unmarshal(capturedBody, &req); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}
	if req.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", req.Status)
	}
	// image_base64 must be absent (omitempty) on failure
	if req.ImageBase64 != "" {
		t.Errorf("expected image_base64 to be absent on failure, got %q", req.ImageBase64)
	}
}
