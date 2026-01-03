package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"gopkg.in/yaml.v3"
)

type Config struct {
	BaendaeliAPIKey  string `yaml:"BAENDAELI_API_KEY"`
	BaendaeliURL     string `yaml:"BAENDAELI_URL"`
	DefaultAmount    int    `yaml:"DEFAULT_AMOUNT_CENTS"`
	SuccessOverlayMs int    `yaml:"SUCCESS_OVERLAY_MILLIS"`
}

type Server struct {
	config     *Config
	httpClient *http.Client
}

type createPaymentPayload struct {
	AmountCents        int    `json:"amount_cents"`
	Currency           string `json:"currency"`
	PaymentRedirectURL string `json:"payment_redirect_url"`
}

func loadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

func main() {
	// Load configuration
	config, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Validate configuration
	if config.BaendaeliAPIKey == "" || config.BaendaeliURL == "" {
		log.Fatal("Configuration missing required fields")
	}

	if config.DefaultAmount == 0 {
		config.DefaultAmount = 2000 // default to 20.00 CHF
	}
	if config.SuccessOverlayMs == 0 {
		config.SuccessOverlayMs = 10000 // 10 seconds by default
	}

	log.Println("Configuration loaded successfully")

	srv := &Server{
		config: config,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}

	// Create router
	r := chi.NewRouter()

	// Add middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Define routes
	r.Get("/", srv.handleIndex)
	r.Post("/api/payment", srv.handleCreatePayment)
	r.Get("/api/payment/{id}", srv.handleGetPaymentStatus)

	// Start server
	addr := ":8000"
	log.Printf("Starting server on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := strings.ReplaceAll(indexPageTemplate, "{{DEFAULT_AMOUNT}}", fmt.Sprintf("%d", s.config.DefaultAmount))
	page = strings.ReplaceAll(page, "{{SUCCESS_OVERLAY_MS}}", fmt.Sprintf("%d", s.config.SuccessOverlayMs))
	_, _ = w.Write([]byte(page))
}

func (s *Server) handleCreatePayment(w http.ResponseWriter, r *http.Request) {
	var payload createPaymentPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		// Allow empty body; fall back to defaults
		payload = createPaymentPayload{}
	}

	if payload.AmountCents == 0 {
		payload.AmountCents = s.config.DefaultAmount
	}
	if payload.Currency == "" {
		payload.Currency = "CHF"
	}
	if payload.PaymentRedirectURL == "" {
		payload.PaymentRedirectURL = "https://example.com/payments/123/complete"
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "failed to prepare payment request", http.StatusInternalServerError)
		return
	}

	targetURL := strings.TrimRight(s.config.BaendaeliURL, "/") + "/api/v1/payment"
	outbound, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, "failed to create outbound request", http.StatusInternalServerError)
		return
	}
	outbound.Header.Set("Authorization", "Bearer "+s.config.BaendaeliAPIKey)
	outbound.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(outbound)
	if err != nil {
		http.Error(w, "failed to reach payment service", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("failed to forward response: %v", err)
	}
}

func (s *Server) handleGetPaymentStatus(w http.ResponseWriter, r *http.Request) {
	paymentID := chi.URLParam(r, "id")
	if paymentID == "" {
		http.Error(w, "payment id is required", http.StatusBadRequest)
		return
	}

	targetURL := strings.TrimRight(s.config.BaendaeliURL, "/") + "/api/v1/payment/" + paymentID
	outbound, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		http.Error(w, "failed to create outbound request", http.StatusInternalServerError)
		return
	}
	outbound.Header.Set("Authorization", "Bearer "+s.config.BaendaeliAPIKey)

	resp, err := s.httpClient.Do(outbound)
	if err != nil {
		http.Error(w, "failed to reach payment service", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("failed to forward status response: %v", err)
	}
}

// Simple inline page to kick off payment creation and render the QR code result.
const indexPageTemplate = `<!doctype html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Baendae.li Payment</title>
	<style>
		:root {
			--bg: #0b1021;
			--panel: #121832;
			--accent: #7ce7c1;
			--muted: #9fb4d1;
			--text: #f7fbff;
		}
		* { box-sizing: border-box; }
		body {
			margin: 0;
			min-height: 100vh;
			display: grid;
			place-items: center;
			background: radial-gradient(circle at 20% 20%, #18224a 0, #0b1021 45%),
									radial-gradient(circle at 80% 10%, #0f2746 0, #0b1021 40%),
									#0b1021;
			font-family: "Space Grotesk", "Segoe UI", system-ui, sans-serif;
			color: var(--text);
		}
		.card {
			width: min(480px, 92vw);
			background: var(--panel);
			border: 1px solid rgba(255,255,255,0.06);
			border-radius: 16px;
			box-shadow: 0 30px 80px rgba(4, 8, 30, 0.55);
			padding: 28px;
			backdrop-filter: blur(6px);
			position: relative;
		}
		h1 {
			margin: 0 0 10px;
			font-size: 26px;
			letter-spacing: 0.3px;
		}
		p {
			margin: 4px 0 14px;
			color: var(--muted);
			line-height: 1.5;
		}
		.status {
			display: inline-flex;
			align-items: center;
			gap: 10px;
			padding: 10px 14px;
			background: rgba(255,255,255,0.04);
			border-radius: 12px;
			border: 1px solid rgba(255,255,255,0.05);
			font-weight: 600;
			letter-spacing: 0.2px;
			color: var(--text);
		}
		.dot {
			width: 12px;
			height: 12px;
			border-radius: 999px;
			background: var(--accent);
			box-shadow: 0 0 0 rgba(124, 231, 193, 0.4);
			animation: pulse 1.2s ease-in-out infinite;
		}
		@keyframes pulse {
			0% { box-shadow: 0 0 0 0 rgba(124, 231, 193, 0.35); }
			70% { box-shadow: 0 0 0 16px rgba(124, 231, 193, 0); }
			100% { box-shadow: 0 0 0 0 rgba(124, 231, 193, 0); }
		}
		.qr-shell {
			margin-top: 20px;
			padding: 16px;
			background: rgba(255,255,255,0.02);
			border-radius: 14px;
			border: 1px dashed rgba(255,255,255,0.08);
			display: grid;
			place-items: center;
			min-height: 220px;
		}
		.qr-shell img {
			max-width: 100%;
			height: auto;
		}
		.error {
			margin-top: 12px;
			color: #ff9b9b;
			font-weight: 600;
		}
		button.hidden { display: none; }
		button.retry {
			margin-top: 10px;
			padding: 10px 14px;
			border-radius: 10px;
			border: 1px solid rgba(255,255,255,0.1);
			background: rgba(255,255,255,0.04);
			color: var(--text);
			cursor: pointer;
			transition: transform 120ms ease, background 150ms ease;
		}
		button.retry:hover { transform: translateY(-1px); background: rgba(255,255,255,0.08); }
		.payment-id {
			position: fixed;
			bottom: 16px;
			right: 16px;
			padding: 6px 10px;
			border-radius: 999px;
			border: 1px solid rgba(255,255,255,0.12);
			background: rgba(255,255,255,0.06);
			color: var(--muted);
			font-size: 11px;
			letter-spacing: 0.2px;
			backdrop-filter: blur(6px);
			z-index: 5;
		}
		.success-banner {
			position: absolute;
			top: 0;
			left: 0;
			right: 0;
			bottom: 0;
			display: grid;
			place-items: center;
			pointer-events: none;
			background: linear-gradient(180deg, rgba(17, 26, 57, 0.85), rgba(11, 16, 33, 0.9));
			opacity: 0;
			transform: scale(0.96);
			transition: opacity 320ms ease, transform 320ms ease;
		}
		.success-banner.animate {
			opacity: 1;
			transform: scale(1);
		}
		.success-pill {
			padding: 22px 26px;
			border-radius: 18px;
			background: linear-gradient(135deg, rgba(124, 231, 193, 0.9), rgba(83, 195, 255, 0.9));
			border: 1px solid rgba(255, 255, 255, 0.35);
			color: #0a1429;
			font-weight: 800;
			font-size: 18px;
			letter-spacing: 0.3px;
			box-shadow: 0 24px 70px rgba(0, 0, 0, 0.55);
			text-align: center;
			max-width: 360px;
		}
		.diagnostics {
			margin-top: 16px;
			padding: 10px 12px;
			border-top: 1px solid rgba(255,255,255,0.08);
			border-radius: 10px;
			background: rgba(255,255,255,0.03);
			display: flex;
			align-items: center;
			justify-content: space-between;
			gap: 12px;
			font-size: 13px;
			color: var(--muted);
		}
		.diagnostics .left {
			display: inline-flex;
			align-items: center;
			gap: 8px;
			font-weight: 700;
			color: var(--text);
		}
		.health-dot-small {
			width: 10px;
			height: 10px;
			border-radius: 999px;
			background: #ffc78d;
			box-shadow: 0 0 0 6px rgba(255, 199, 141, 0.12);
			transition: background 160ms ease, box-shadow 200ms ease;
		}
		.health-dot-small.ok {
			background: #7ce7c1;
			box-shadow: 0 0 0 6px rgba(124, 231, 193, 0.18);
		}
		.health-dot-small.bad {
			background: #ff9b9b;
			box-shadow: 0 0 0 6px rgba(255, 155, 155, 0.18);
		}
		.health-dot-small.pending {
			background: #ffc78d;
			box-shadow: 0 0 0 6px rgba(255, 199, 141, 0.12);
		}
		.diagnostics .meta {
			font-size: 12px;
			color: var(--muted);
			text-align: right;
			display: flex;
			flex-direction: column;
			align-items: flex-end;
			gap: 4px;
		}
		#expiryMeta {
			color: var(--text);
			font-weight: 700;
		}
	</style>
</head>
<body>
	<div class="card">
			<div class="success-banner hidden" id="successBanner">
				<div class="success-pill">Danke, dein Solibändeli wird ausgegeben!</div>
			</div>
		<h1>Bändäli Outomat</h1>
		<p>Scanne den QR-Code mit deinem Smartphone, um das Solibändeli zu bezahlen.</p>
		<div class="status" id="status"><span class="dot"></span><span>Wait: creating payment...</span></div>
		<div class="qr-shell" id="qr">Warten auf QR Code...</div>
		<div class="error" id="error"></div>
		<button class="retry hidden" id="retry">Try again</button>
		<div class="diagnostics" id="diagnostics">
			<div class="left"><span class="health-dot-small pending" id="gatewayDot"></span><span id="gatewayStatusText">Gateway wird geprüft...</span></div>
			<div class="meta">
				<div id="gatewayMeta">Warte auf erste Antwort</div>
				<div id="expiryMeta">Gültig für --:--</div>
			</div>
		</div>
	</div>
	<div class="payment-id hidden" id="paymentId"></div>

	<script>
		const statusEl = document.getElementById('status');
		const qrEl = document.getElementById('qr');
		const errorEl = document.getElementById('error');
		const retryBtn = document.getElementById('retry');
		const paymentIdEl = document.getElementById('paymentId');
		const successBanner = document.getElementById('successBanner');
		const gatewayDot = document.getElementById('gatewayDot');
		const gatewayStatusText = document.getElementById('gatewayStatusText');
		const gatewayMeta = document.getElementById('gatewayMeta');
		const expiryMeta = document.getElementById('expiryMeta');
		const defaultAmount = {{DEFAULT_AMOUNT}};
		const successOverlayMs = {{SUCCESS_OVERLAY_MS}};
			let pollTimer = null;
			let currentPaymentId = null;
			let lastDiagnosticsState = 'pending';
			let lastDiagnosticsTime = null;
			let lastDiagnosticsLatency = null;
			let expiryAt = null;
			let expiryTimer = null;

		retryBtn.addEventListener('click', () => {
			retryBtn.classList.add('hidden');
			errorEl.textContent = '';
			qrEl.textContent = 'Warten auf QR Code...';
			paymentIdEl.classList.add('hidden');
				clearPoll();
				clearExpiry();
			start();
		});

		function clearExpiry() {
			if (expiryTimer) {
				clearInterval(expiryTimer);
				expiryTimer = null;
			}
			expiryAt = null;
			expiryMeta.textContent = 'Gültig für --:--';
		}

		function startExpiryCountdown(expiresAtString, validForMinutes) {
			clearExpiry();
			let target = null;
			if (expiresAtString) {
				const parsed = Date.parse(expiresAtString);
				if (!Number.isNaN(parsed)) {
					target = parsed;
				}
			}
			if (!target && validForMinutes) {
				target = Date.now() + Math.max(0, validForMinutes) * 60_000;
			}
			if (!target) return;
			expiryAt = target;
			updateExpiryCountdown();
			expiryTimer = setInterval(updateExpiryCountdown, 1000);
		}

		function updateExpiryCountdown() {
			if (!expiryAt) return;
			const remaining = expiryAt - Date.now();
			if (remaining <= 0) {
				clearExpiry();
				start();
				return;
			}
			const mins = Math.floor(remaining / 60_000);
			const secs = Math.floor((remaining % 60_000) / 1_000);
			expiryMeta.textContent = 'Gültig für ' + String(mins).padStart(2, '0') + ':' + String(secs).padStart(2, '0');
		}

		function setDiagnosticsPending() {
			lastDiagnosticsState = 'pending';
			lastDiagnosticsTime = null;
			lastDiagnosticsLatency = null;
			gatewayDot.className = 'health-dot-small pending';
			gatewayStatusText.textContent = 'Gateway wird geprüft...';
			gatewayMeta.textContent = 'Warte auf erste Antwort';
			expiryMeta.textContent = 'Gültig für --:--';
		}

		function updateDiagnostics({ ok, latencyMs, at }) {
			lastDiagnosticsState = ok ? 'ok' : 'bad';
			lastDiagnosticsTime = at || Date.now();
			lastDiagnosticsLatency = latencyMs != null ? Math.max(0, Math.round(latencyMs)) : null;

			if (ok) {
				gatewayDot.className = 'health-dot-small ok';
				gatewayStatusText.textContent = 'Gateway: OK';
				const latencyText = lastDiagnosticsLatency != null ? lastDiagnosticsLatency + ' ms' : '—';
				gatewayMeta.textContent = 'Zuletzt: ' + formatTime(lastDiagnosticsTime) + ' · Latenz: ' + latencyText;
			} else {
				gatewayDot.className = 'health-dot-small bad';
				gatewayStatusText.textContent = 'Gateway: gestört';
				gatewayMeta.textContent = 'Letzter Versuch: ' + formatTime(lastDiagnosticsTime);
			}
		}

		function formatTime(ts) {
			if (!ts) return '-';
			try {
				return new Date(ts).toLocaleTimeString('de-CH', { hour12: false });
			} catch {
				return '-';
			}
		}

		setDiagnosticsPending();

			function clearPoll() {
				if (pollTimer) {
					clearTimeout(pollTimer);
					pollTimer = null;
				}
			}

		async function safeParseJson(res) {
			const text = await res.text();
			try {
				return { data: JSON.parse(text), raw: text };
			} catch {
				return { data: {}, raw: text };
			}
		}

		async function start() {
			statusEl.innerHTML = '<span class="dot"></span><span>Wait: creating payment...</span>';
			paymentIdEl.classList.add('hidden');
			errorEl.textContent = '';
			successBanner.classList.add('hidden');
			successBanner.classList.remove('animate');
			qrEl.classList.remove('hidden');
			clearPoll();
			clearExpiry();
			setDiagnosticsPending();
			try {
				const res = await fetch('/api/payment', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({
						amount_cents: defaultAmount,
						currency: 'CHF',
						payment_redirect_url: 'https://example.com/payments/123/complete'
					})
				});

				const { data } = await safeParseJson(res);

				if (!res.ok) {
					throw new Error(data.error || 'Payment creation failed');
				}

				statusEl.innerHTML = '<span class="dot"></span><span>Payment created</span>';
				if (data.id) {
					paymentIdEl.textContent = 'ID ' + data.id;
					paymentIdEl.classList.remove('hidden');
					currentPaymentId = data.id;
					pollStatus(data.id);
				}
				startExpiryCountdown(data.expires_at, data.valid_for_minutes);
				renderQr(data);
			} catch (err) {
				console.error('Payment creation failed', err);
				statusEl.innerHTML = '<span class="dot"></span><span>Etwas ist schiefgelaufen. Bitte versuche es erneut.</span>';
				errorEl.textContent = 'Die Zahlung konnte nicht gestartet werden. Bitte versuche es erneut.';
				retryBtn.classList.remove('hidden');
			}
		}

		async function pollStatus(id) {
			if (!id) return;
			clearPoll();
			const attemptStarted = performance.now();
			let diagUpdated = false;
			try {
				const res = await fetch('/api/payment/' + encodeURIComponent(id));
				const latencyMs = Math.round(performance.now() - attemptStarted);
				const timestamp = Date.now();
				const { data } = await safeParseJson(res);

				if (!res.ok) {
					updateDiagnostics({ ok: false, latencyMs, at: timestamp });
					diagUpdated = true;
					throw new Error(data.error || 'Status check failed');
				}

				updateDiagnostics({ ok: true, latencyMs, at: timestamp });
				diagUpdated = true;
				errorEl.textContent = '';

				const status = (data.status || '').toLowerCase();
				switch (status) {
					case 'waiting':
						statusEl.innerHTML = '<span class="dot"></span><span>Warten auf Zahlung...</span>';
						pollTimer = setTimeout(() => pollStatus(id), 2000);
						break;
					case 'success':
						statusEl.innerHTML = '<span class="dot"></span><span>Zahlung erfolgreich</span>';
						showSuccessThenRestart();
						break;
					case 'failure':
						statusEl.innerHTML = '<span class="dot"></span><span>Zahlung fehlgeschlagen - versuche es erneut</span>';
						setTimeout(() => start(), 1200);
						break;
					default:
						statusEl.innerHTML = '<span class="dot"></span><span>Unbekannter Status</span>';
						pollTimer = setTimeout(() => pollStatus(id), 3000);
				}
			} catch (err) {
				console.error('Status check failed', err);
				if (!diagUpdated) {
					updateDiagnostics({ ok: false, latencyMs: null, at: Date.now() });
				}
				errorEl.textContent = 'Der Zahlungsstatus konnte nicht geprüft werden. Wir versuchen es gleich erneut.';
				statusEl.innerHTML = '<span class="dot"></span><span>Status wird erneut überprüft...</span>';
				pollTimer = setTimeout(() => pollStatus(id), 3000);
			}
		}

		function showSuccessThenRestart() {
			successBanner.classList.remove('hidden');
			successBanner.classList.add('animate');
			qrEl.classList.add('hidden');
			setTimeout(() => {
				successBanner.classList.remove('animate');
				successBanner.classList.add('hidden');
				start();
			}, successOverlayMs);
		}

		function renderQr(data) {
			const svg = data.qr_code_svg || data.qrcode_svg || data.qr_svg || data.twint_qr_code_svg;
			const png = data.qr_code_png_base64 || data.qrcode_png_base64 || data.twint_qr_code_png_base64;
			const url = data.qr_code_url || data.qrcode_url || data.payment_qr_url || data.url;
			const qrData = data.qr || data.qrcode || data.qr_data;

			qrEl.innerHTML = '';

			if (svg && typeof svg === 'string') {
				qrEl.innerHTML = svg;
				return;
			}

			if (png && typeof png === 'string') {
				const img = new Image();
				img.src = 'data:image/png;base64,' + png;
				img.alt = 'Payment QR code';
				qrEl.appendChild(img);
				return;
			}

			if (qrData && typeof qrData === 'string') {
				const trimmed = qrData.trim();
				// If the API returns a data URL (e.g., data:text/plain;base64,...), use it directly.
				if (trimmed.startsWith('data:')) {
					const img = new Image();
					img.src = trimmed;
					img.alt = 'Payment QR code';
					qrEl.appendChild(img);
					return;
				}

				// If the API returns inline SVG markup, render it as HTML.
				if (trimmed.startsWith('<svg')) {
					qrEl.innerHTML = trimmed;
					return;
				}

				// If we have a bare base64 payload, treat it as PNG data.
				const base64Like = /^[A-Za-z0-9+/]+={0,2}$/;
				if (base64Like.test(trimmed)) {
					const img = new Image();
					img.src = 'data:image/png;base64,' + trimmed;
					img.alt = 'Payment QR code';
					qrEl.appendChild(img);
					return;
				}
			}

			if (url && typeof url === 'string') {
				const img = new Image();
				img.src = url;
				img.alt = 'Payment QR code';
				qrEl.appendChild(img);
				return;
			}

			qrEl.textContent = 'No QR code found in the response.';
		}

		start();
	</script>
</body>
</html>`
