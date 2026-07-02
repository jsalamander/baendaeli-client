package server

import (
    "encoding/json"
    "io"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/jsalamander/baendaeli-client/internal/config"
    "github.com/jsalamander/baendaeli-client/internal/device"
)

// helper to build server with injectable HTTP client
func newTestServer(cfg *config.Config, client *http.Client) *Server {
    s := New(cfg)
    if client != nil {
        s.httpClient = client
    }
    return s
}

func TestHandleCreatePayment_ForwardsPayloadAndDefaults(t *testing.T) {
    // backend mock
    backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            t.Fatalf("expected POST, got %s", r.Method)
        }
        if r.URL.Path != "/api/v1/payment" {
            t.Fatalf("unexpected path: %s", r.URL.Path)
        }
        if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
            t.Fatalf("expected bearer token, got %q", got)
        }

        body, _ := io.ReadAll(r.Body)
        var payload map[string]any
        if err := json.Unmarshal(body, &payload); err != nil {
            t.Fatalf("payload not JSON: %v", err)
        }

        if payload["currency"] != "CHF" {
            t.Fatalf("currency mismatch: %v", payload["currency"])
        }
        if payload["payment_redirect_url"] == "" {
            t.Fatalf("redirect URL should not be empty")
        }

        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusCreated)
        w.Write([]byte(`{"id":"abc123","status":"waiting"}`))
    }))
    defer backend.Close()

    cfg := &config.Config{
        BaendaeliAPIKey: "test-key",
        BaendaeliURL:    backend.URL,
    }
    srv := newTestServer(cfg, backend.Client())

    rr := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodPost, "/api/payment", nil)
    srv.Router().ServeHTTP(rr, req)

    if rr.Code != http.StatusCreated {
        t.Fatalf("unexpected status: %d", rr.Code)
    }
    if !strings.Contains(rr.Body.String(), "abc123") {
        t.Fatalf("response body missing id: %s", rr.Body.String())
    }
}

func TestHandleGetPaymentStatus_ForwardsPathAndHeaders(t *testing.T) {
    backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
            t.Fatalf("expected GET, got %s", r.Method)
        }
        if r.URL.Path != "/api/v1/payment/pay-789" {
            t.Fatalf("unexpected path: %s", r.URL.Path)
        }
        if got := r.Header.Get("Authorization"); got != "Bearer secret" {
            t.Fatalf("expected bearer token, got %q", got)
        }
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"waiting"}`))
    }))
    defer backend.Close()

    cfg := &config.Config{
        BaendaeliAPIKey: "secret",
        BaendaeliURL:    backend.URL,
    }
    srv := newTestServer(cfg, backend.Client())

    rr := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodGet, "/api/payment/pay-789", nil)
    srv.Router().ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("unexpected status: %d", rr.Code)
    }
    if !strings.Contains(rr.Body.String(), "waiting") {
        t.Fatalf("response body missing status: %s", rr.Body.String())
    }
}

func TestServeMainTemplate_SubstitutesConfig(t *testing.T) {
    cfg := &config.Config{DefaultAmount: 3210, SuccessOverlayMs: 5555}
    srv := newTestServer(cfg, nil)

    rr := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodGet, "/main.js", nil)
    srv.Router().ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("unexpected status: %d", rr.Code)
    }
    body := rr.Body.String()
    if !strings.Contains(body, "const defaultAmount = 3210") {
        t.Fatalf("defaultAmount not substituted: %s", body)
    }
    if !strings.Contains(body, "const successOverlayMs = 5555") {
        t.Fatalf("successOverlayMs not substituted: %s", body)
    }
    if !strings.Contains(body, "fetch('/api/device/status')") {
        t.Fatalf("main.js should poll device status endpoint, body: %s", body)
    }
    if strings.Contains(body, "createPayment(") {
        t.Fatalf("main.js should not orchestrate payment creation anymore, body: %s", body)
    }
    if !strings.Contains(body, "renderQr(data.payment)") {
        t.Fatalf("main.js should render QR from device status payment payload, body: %s", body)
    }
    if !strings.Contains(body, "renderPaymentExpiry(data.payment, state)") {
        t.Fatalf("main.js should pass state into renderPaymentExpiry, body: %s", body)
    }
    if !strings.Contains(body, "payment.amount_selection_expires_at") {
        t.Fatalf("main.js should read amount_selection_expires_at for ball_detected, body: %s", body)
    }
    if !strings.Contains(body, "payment.payment_expires_at") {
        t.Fatalf("main.js should read payment_expires_at for awaiting_payment, body: %s", body)
    }
    if !strings.Contains(body, "Warte auf Betragsauswahl · ") {
        t.Fatalf("main.js should show waiting-for-amount countdown in overlay, body: %s", body)
    }
    if strings.Contains(body, "renderQrPlaceholder('Betrag wird ausgewählt'") {
        t.Fatalf("main.js should not duplicate waiting-for-amount countdown text in qr placeholder, body: %s", body)
    }
    if !strings.Contains(body, "state === 'awaiting_payment'") {
        t.Fatalf("main.js should handle awaiting_payment overlay branch, body: %s", body)
    }
    if !strings.Contains(body, "Warte auf Zahlung · ") {
        t.Fatalf("main.js should show waiting-for-payment countdown in overlay, body: %s", body)
    }
    if strings.Contains(body, "renderQrPlaceholder('Warte auf Zahlung'") {
        t.Fatalf("main.js should not duplicate waiting-for-payment countdown text in qr placeholder, body: %s", body)
    }
    if strings.Contains(body, "payment.expires_at") || strings.Contains(body, "payment.expiration_at") {
        t.Fatalf("main.js should not reference removed expires_at fields, body: %s", body)
    }
    if strings.Contains(body, "valid_for_minutes") {
        t.Fatalf("main.js should not use valid_for_minutes countdown fallback anymore, body: %s", body)
    }
    if strings.Contains(body, "state === 'ball_detected' || state === 'awaiting_payment'") {
        t.Fatalf("main.js should not keep QR visible once awaiting_payment starts, body: %s", body)
    }
}

func TestServeStaticJS(t *testing.T) {
    cfg := &config.Config{}
    srv := newTestServer(cfg, nil)

    rr := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodGet, "/ui.js", nil)
    srv.Router().ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("unexpected status: %d", rr.Code)
    }
    if !strings.Contains(rr.Body.String(), "function updateStatus") {
        t.Fatalf("ui.js content unexpected: %s", rr.Body.String())
    }
}

func TestHandleDeviceStatusWithoutDeviceClient(t *testing.T) {
    cfg := &config.Config{}
    srv := newTestServer(cfg, nil)

    rr := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodGet, "/api/device/status", nil)
    srv.Router().ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("unexpected status: %d", rr.Code)
    }

    var body map[string]any
    if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
        t.Fatalf("failed to parse response: %v", err)
    }
    if body["state"] != "idle" {
        t.Fatalf("expected state=idle, got %v", body["state"])
    }
    if body["jammed"] != false {
        t.Fatalf("expected jammed=false, got %v", body["jammed"])
    }
    if _, ok := body["pending_command"]; !ok {
        t.Fatal("expected pending_command field in default device status")
    }
}

func TestHandleDeviceStatusWithDeviceClientSnapshot(t *testing.T) {
    cfg := &config.Config{}
    srv := newTestServer(cfg, nil)
    dc := device.New(cfg)
    dc.SetPaymentID("pay-777")
    srv.SetDeviceClient(dc)

    rr := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodGet, "/api/device/status", nil)
    srv.Router().ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("unexpected status: %d", rr.Code)
    }

    var body map[string]any
    if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
        t.Fatalf("failed to parse response: %v", err)
    }
    if body["payment_id"] != "pay-777" {
        t.Fatalf("expected payment_id pay-777, got %v", body["payment_id"])
    }
    if _, ok := body["state"]; !ok {
        t.Fatal("expected state field in device snapshot")
    }
    if _, ok := body["jammed"]; !ok {
        t.Fatal("expected jammed field in device snapshot")
    }
}
