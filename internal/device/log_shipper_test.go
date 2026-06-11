package device

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jsalamander/baendaeli-client/internal/config"
)

func newTestClient(serverURL string) *Client {
	cfg := &config.Config{
		BaendaeliURL:               serverURL,
		BaendaeliAPIKey:            "test-key",
		LogShippingEnabled:         true,
		LogShippingFlushIntervalMs: 100,
		LogShippingBatchLines:      100,
		LogShippingMaxQueueLines:   1000,
		LogShippingMaxLineBytes:    16384,
		LogShippingMaxRequestBytes: 262144,
		ColorSensorEnabled:         false,
		ActuatorEnabled:            false,
		VibrationEnabled:           false,
		CameraEnabled:              false,
	}
	cfg.SetDefaults()
	return New(cfg)
}

func TestLogShipperSendsNDJSONAndAuth(t *testing.T) {
	var receivedBody string
	var receivedAuth string
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/device/logs" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		receivedAuth = r.Header.Get("Authorization")
		receivedContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"accepted_lines":2,"written_bytes":200,"filename":"test.log"}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	shipper := newLogShipper(context.Background(), client, client.httpClient, io.Discard)

	shipper.enqueue("first line")
	shipper.enqueue("second line")

	if err := shipper.flushOnce(context.Background()); err != nil {
		t.Fatalf("flushOnce failed: %v", err)
	}

	if receivedAuth != "Bearer test-key" {
		t.Fatalf("expected bearer auth, got %q", receivedAuth)
	}
	if receivedContentType != "application/x-ndjson" {
		t.Fatalf("expected ndjson content-type, got %q", receivedContentType)
	}

	lines := strings.Split(strings.TrimSpace(receivedBody), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 NDJSON lines, got %d", len(lines))
	}

	for _, line := range lines {
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
		if payload["level"] == "" || payload["time"] == "" || payload["message"] == "" {
			t.Fatalf("missing required fields in payload: %+v", payload)
		}
	}

	if len(shipper.queue) != 0 {
		t.Fatalf("expected queue to be empty after successful flush, got %d", len(shipper.queue))
	}
}

func TestLogShipperStopsOnUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	shipper := newLogShipper(context.Background(), client, client.httpClient, io.Discard)
	shipper.enqueue("line")

	if err := shipper.flushOnce(context.Background()); err != nil {
		t.Fatalf("expected nil error for auth failure disable, got %v", err)
	}

	if !shipper.isDisabled() {
		t.Fatal("expected shipper to be disabled after 401")
	}
	if len(shipper.queue) != 1 {
		t.Fatalf("expected queued lines to remain, got %d", len(shipper.queue))
	}
}

func TestLogShipperDropsInvalidSingleLineOn422(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"invalid payload"}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	shipper := newLogShipper(context.Background(), client, client.httpClient, io.Discard)
	shipper.enqueue("line")

	if err := shipper.flushOnce(context.Background()); err != nil {
		t.Fatalf("expected nil error after dropping invalid line, got %v", err)
	}

	if len(shipper.queue) != 0 {
		t.Fatalf("expected queue to be empty after dropping invalid line, got %d", len(shipper.queue))
	}
}

func TestLogShipperRetainsQueueOnTransientFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server error"}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	shipper := newLogShipper(context.Background(), client, client.httpClient, io.Discard)
	shipper.enqueue("line")

	if err := shipper.flushOnce(context.Background()); err == nil {
		t.Fatal("expected transient error from flushOnce")
	}
	if len(shipper.queue) != 1 {
		t.Fatalf("expected queue to be retained, got %d", len(shipper.queue))
	}
}
