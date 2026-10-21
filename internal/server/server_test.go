package server

import (
    "bytes"
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
    if !strings.Contains(body, "Scanne QR Code mit der KAMERA 📷 und zahle anschliessend mit Twint auf der Webseite") {
        t.Fatalf("main.js should show the updated camera and Twint instructions, body: %s", body)
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
    if !strings.Contains(body, "if (data.payment && state === 'ball_detected') {") {
        t.Fatalf("main.js should keep the QR visible while waiting for amount selection, body: %s", body)
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

func TestServeAnimalImage(t *testing.T) {
    srv := newTestServer(&config.Config{}, nil)

    rr := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodGet, "/animals/owl-01.png", nil)
    srv.Router().ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("unexpected status: %d", rr.Code)
    }
    if got := rr.Header().Get("Content-Type"); got != "image/png" {
        t.Fatalf("unexpected content type: %q", got)
    }
    if !bytes.HasPrefix(rr.Body.Bytes(), []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
        t.Fatal("animal image response is not a PNG")
    }
}

func TestHandleIndexShowsAnimalDecorations(t *testing.T) {
    srv := newTestServer(&config.Config{}, nil)

    rr := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodGet, "/", nil)
    srv.Router().ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("unexpected status: %d", rr.Code)
    }
    body := rr.Body.String()
    qrEnd := strings.Index(body, `id="qr"`)
    prizeInstruction := strings.Index(body, `id="prizeInstruction">Wenn du eine Nummer ziehst, kannst du diese gegen einen Gewinn an der Aussenbar eintauschen.</p>`)
    if qrEnd < 0 || prizeInstruction < qrEnd {
        t.Fatal("prize instruction should appear below the QR code")
    }
    if got := strings.Count(body, `src="/animals/`); got != 8 {
        t.Fatalf("expected 8 animal images, got %d", got)
    }
    if got := strings.Count(body, `<span class="animal-speech">niedliche niete</span>`); got != 8 {
        t.Fatalf("expected 8 animal speech bubbles, got %d", got)
    }
    if !strings.Contains(body, "@keyframes animal-flight") || !strings.Contains(body, "prefers-reduced-motion: reduce") {
        t.Fatal("animal decorations should fly and respect reduced-motion preferences")
    }
    if !strings.Contains(body, "animation-duration: 9s; animation-delay: -9s;") ||
        !strings.Contains(body, "animation-duration: 19s; animation-delay: -3s;") ||
        !strings.Contains(body, "42% { translate: var(--flight-x3) var(--flight-y3); }") ||
        !strings.Contains(body, "--flight-y1: 1vh;\n\t\t\t--flight-x2: 12vw; --flight-y2: 5vh;\n\t\t\t--flight-x3: 8vw; --flight-y3: 9vh;\n\t\t\t--flight-x4: 1vw; --flight-y4: 8vh;") {
        t.Fatal("animals should use varied multi-direction flight paths and speeds")
    }
    if !strings.Contains(body, "nav {\n\t\t\tposition: relative;\n\t\t\tz-index: 1;") ||
        !strings.Contains(body, "#animalDecorations {\n\t\t\tposition: fixed;\n\t\t\tz-index: 2;") ||
        !strings.Contains(body, "#mainContent {\n\t\t\tposition: relative;\n\t\t\tz-index: 3;") {
        t.Fatal("animal decorations should appear over the navbar but remain behind main content")
    }
    if !strings.Contains(body, "@keyframes prize-flight") ||
        !strings.Contains(body, "animation: prize-flight 8s") ||
        !strings.Contains(body, "animation: prize-flight 13s") ||
        !strings.Contains(body, "top: 12%; left: 5%") ||
        !strings.Contains(body, "bottom: 12%; right: 5%") {
        t.Fatal("prize slips should follow varied flight paths around the screen edges")
    }
    if got := strings.Count(body, `class="prize-slip" aria-hidden="true"`); got != 4 {
        t.Fatalf("expected 4 flying prize slips, got %d", got)
    }
    if got := strings.Count(body, `<span class="prize-slip-label">Gewinn</span>`); got != 4 {
        t.Fatalf("expected 4 prize slip labels, got %d", got)
    }
    mainEnd := strings.Index(body, "</main>")
    overlayStart := strings.Index(body, `id="successBanner"`)
    animalsStart := strings.Index(body, `id="animalDecorations"`)
    if mainEnd < 0 || overlayStart < mainEnd || animalsStart < overlayStart {
        t.Fatal("overlays must be outside the main content and above the animal decorations")
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
    cfg := &config.Config{LeverEnabled: true, LeverUpPin: "GPIO26", LeverDownPin: "GPIO21"}
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
    if body["state"] != string(device.StateStarting) {
        t.Fatalf("enabling the lever must not change the initial device state, got %v", body["state"])
    }
    for key := range body {
        switch key {
        case "state", "message", "payment_id", "payment", "jammed", "executing_command", "pending_command":
        default:
            t.Fatalf("lever configuration must preserve the device status response shape, unexpected key %q", key)
        }
    }
}
