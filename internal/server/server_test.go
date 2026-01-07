package server

import (
    "encoding/json"
    "io"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/jsalamander/baendaeli-client/internal/config"
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

        if payload["amount_cents"] != float64(1234) { // json numbers decode as float64
            t.Fatalf("amount_cents mismatch: %v", payload["amount_cents"])
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
        DefaultAmount:   1234,
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
