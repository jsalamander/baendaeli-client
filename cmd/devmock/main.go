package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
)

type payment struct {
	ID                       string     `json:"id"`
	Status                   string     `json:"status"`
	PaymentPhase             string     `json:"payment_phase"`
	AmountCents              *int64     `json:"amount_cents"`
	AmountSelectionExpiresAt *time.Time `json:"amount_selection_expires_at"`
	PaymentExpiresAt         *time.Time `json:"payment_expires_at"`
	QRCodePNGBase64          string     `json:"qr_code_png_base64"`
	CreatedAt                time.Time  `json:"created_at"`
	QRScanned                bool       `json:"-"`
}

type paymentSummary struct {
	ID                       string     `json:"id"`
	Status                   string     `json:"status"`
	PaymentPhase             string     `json:"payment_phase"`
	AmountCents              *int64     `json:"amount_cents"`
	AmountSelectionExpiresAt *time.Time `json:"amount_selection_expires_at"`
	PaymentExpiresAt         *time.Time `json:"payment_expires_at"`
	CreatedAt                time.Time  `json:"created_at"`
	QRScanned                bool       `json:"qr_scanned"`
}

type deviceReport struct {
	PaymentID     *string `json:"payment_id"`
	ClientVersion string  `json:"client_version"`
	Dispensed     int     `json:"dispensed_count"`
}

type mockAPI struct {
	mu               sync.RWMutex
	baseURL          string
	payments         map[string]*payment
	order            []string
	nextPaymentID    int
	statusReports    int
	logLines         int
	lastReport       deviceReport
	lastTerminalAt   time.Time
	nextPaymentDelay time.Duration
}

func newMockAPI(baseURL string) *mockAPI {
	return &mockAPI{
		baseURL:          strings.TrimRight(baseURL, "/"),
		payments:         make(map[string]*payment),
		nextPaymentDelay: 5 * time.Second,
	}
}

func (a *mockAPI) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.dashboard)
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /dev/state", a.state)
	mux.HandleFunc("POST /dev/payment/{id}/scanned", a.markQRScanned)
	mux.HandleFunc("POST /dev/payment/{id}/amount", a.selectAmount)
	mux.HandleFunc("POST /dev/payment/{id}/paid", a.markPaid)
	mux.HandleFunc("POST /dev/payment/{id}/fail", a.markFailed)
	mux.HandleFunc("POST /dev/payment/{id}/cancel", a.markCancelled)
	mux.HandleFunc("POST /api/v1/payment", a.createPayment)
	mux.HandleFunc("GET /api/v1/payment/{id}", a.getPayment)
	mux.HandleFunc("POST /api/v1/device/status", a.reportDeviceStatus)
	mux.HandleFunc("GET /api/v1/device/commands", a.getCommand)
	mux.HandleFunc("POST /api/v1/device/commands/{id}/ack", a.ackCommand)
	mux.HandleFunc("POST /api/v1/device/logs", a.receiveLogs)
	return mux
}

func (a *mockAPI) dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, dashboardHTML)
}

func (a *mockAPI) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *mockAPI) state(w http.ResponseWriter, _ *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	payments := make([]paymentSummary, 0, len(a.order))
	for i := len(a.order) - 1; i >= 0; i-- {
		if current := a.payments[a.order[i]]; current != nil {
			payments = append(payments, summarizePayment(current))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"payments":       payments,
		"status_reports": a.statusReports,
		"log_lines":      a.logLines,
		"last_report":    a.lastReport,
	})
}

func (a *mockAPI) createPayment(w http.ResponseWriter, r *http.Request) {
	if !a.waitForNextPaymentDelay(r.Context()) {
		return
	}

	a.mu.Lock()
	a.nextPaymentID++
	id := fmt.Sprintf("mock-%06d", a.nextPaymentID)
	createdAt := time.Now().UTC()
	qrTarget := a.baseURL + "/?payment=" + url.QueryEscape(id)
	qrPNG, err := qrcode.Encode(qrTarget, qrcode.Medium, 300)
	if err != nil {
		a.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not generate local QR code"})
		return
	}
	created := &payment{
		ID:              id,
		Status:          "waiting",
		PaymentPhase:    "waiting_for_amount",
		QRCodePNGBase64: base64.StdEncoding.EncodeToString(qrPNG),
		CreatedAt:       createdAt,
	}
	a.payments[id] = created
	a.order = append(a.order, id)
	response := *created
	a.mu.Unlock()

	writeJSON(w, http.StatusCreated, response)
}

func (a *mockAPI) waitForNextPaymentDelay(ctx context.Context) bool {
	for {
		a.mu.RLock()
		lastTerminalAt := a.lastTerminalAt
		delay := a.nextPaymentDelay
		a.mu.RUnlock()
		if lastTerminalAt.IsZero() || delay <= 0 {
			return true
		}

		remaining := time.Until(lastTerminalAt.Add(delay))
		if remaining <= 0 {
			return true
		}

		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

func (a *mockAPI) getPayment(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	current := a.payments[r.PathValue("id")]
	if current != nil {
		response := *current
		a.mu.RUnlock()
		writeJSON(w, http.StatusOK, response)
		return
	}
	a.mu.RUnlock()
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "payment not found"})
}

func (a *mockAPI) selectAmount(w http.ResponseWriter, r *http.Request) {
	var request struct {
		AmountCents int64 `json:"amount_cents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.AmountCents <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "amount_cents must be a positive integer"})
		return
	}

	a.mu.Lock()
	current := a.payments[r.PathValue("id")]
	if current == nil {
		a.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "payment not found"})
		return
	}
	if current.Status != "waiting" || current.PaymentPhase != "waiting_for_amount" {
		a.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "payment is not waiting for an amount"})
		return
	}
	current.AmountCents = &request.AmountCents
	current.PaymentPhase = "waiting_for_payment"
	expiresAt := time.Now().UTC().Add(5 * time.Minute)
	current.PaymentExpiresAt = &expiresAt
	response := *current
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, response)
}

func (a *mockAPI) markQRScanned(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	current := a.payments[r.PathValue("id")]
	if current == nil {
		a.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "payment not found"})
		return
	}
	if current.Status != "waiting" || current.PaymentPhase != "waiting_for_amount" {
		a.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "payment is not waiting for an amount"})
		return
	}
	if !current.QRScanned {
		current.QRScanned = true
		expiresAt := time.Now().UTC().Add(2 * time.Minute)
		current.AmountSelectionExpiresAt = &expiresAt
	}
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"success": true, "qr_scanned": true})
}

func (a *mockAPI) markPaid(w http.ResponseWriter, r *http.Request) {
	a.setTerminalPaymentState(w, r, "paid")
}

func (a *mockAPI) markFailed(w http.ResponseWriter, r *http.Request) {
	a.setTerminalPaymentState(w, r, "failed")
}

func (a *mockAPI) markCancelled(w http.ResponseWriter, r *http.Request) {
	a.setTerminalPaymentState(w, r, "cancelled")
}

func (a *mockAPI) setTerminalPaymentState(w http.ResponseWriter, r *http.Request, status string) {
	a.mu.Lock()
	current := a.payments[r.PathValue("id")]
	if current == nil {
		a.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "payment not found"})
		return
	}
	if current.Status != "waiting" || current.PaymentPhase != "waiting_for_payment" {
		a.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "select an amount before changing payment status"})
		return
	}
	current.Status = status
	a.lastTerminalAt = time.Now()
	response := *current
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, response)
}

func (a *mockAPI) reportDeviceStatus(w http.ResponseWriter, r *http.Request) {
	var report deviceReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid device status"})
		return
	}
	a.mu.Lock()
	a.statusReports++
	a.lastReport = report
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (a *mockAPI) getCommand(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"command": ""})
}

func (a *mockAPI) ackCommand(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (a *mockAPI) receiveLogs(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read log batch"})
		return
	}
	lines := 0
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	a.mu.Lock()
	a.logLines += lines
	a.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{
		"success":        true,
		"accepted_lines": lines,
		"written_bytes":  len(body),
		"filename":       "local-dev-mock",
	})
}

func summarizePayment(current *payment) paymentSummary {
	return paymentSummary{
		ID:                       current.ID,
		Status:                   current.Status,
		PaymentPhase:             current.PaymentPhase,
		AmountCents:              current.AmountCents,
		AmountSelectionExpiresAt: current.AmountSelectionExpiresAt,
		PaymentExpiresAt:         current.PaymentExpiresAt,
		CreatedAt:                current.CreatedAt,
		QRScanned:                current.QRScanned,
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8001", "local address for the payment API mock and control dashboard")
	flag.Parse()

	api := newMockAPI("http://" + *addr)
	server := &http.Server{
		Addr:              *addr,
		Handler:           api.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Local payment API mock and controls: http://%s", *addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Payment mock server error: %v", err)
	}
}

const dashboardHTML = `<!doctype html>
<html lang="en" data-theme="light">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>Local Payment Simulator</title>
	<style>
		:root { color-scheme: light; --paper: #f3f1ef; --panel: #e5e2df; --ink: #101010; --red: #f10d1b; --green: #228b22; }
		* { box-sizing: border-box; }
		body { margin: 0; background: var(--paper); color: var(--ink); font: 16px/1.5 system-ui, sans-serif; }
		main { width: min(900px, 100%); margin: 0 auto; padding: 24px 16px 48px; }
		header { display: flex; justify-content: space-between; align-items: center; gap: 16px; border-bottom: 2px solid var(--ink); padding-bottom: 16px; }
		h1 { margin: 0; font-size: 1.5rem; }
		.notice { margin: 16px 0 24px; padding: 12px 16px; background: var(--panel); }
		#payments { display: grid; gap: 12px; }
		.payment { border: 1px solid #b7b2ae; background: #fff; padding: 16px; }
		.payment h2 { margin: 0 0 4px; font-size: 1.1rem; }
		.meta { margin: 0 0 14px; color: #555; }
		.actions { display: flex; flex-wrap: wrap; gap: 8px; }
		button { border: 1px solid var(--ink); background: #fff; color: var(--ink); padding: 8px 12px; font: inherit; cursor: pointer; }
		button:hover { background: var(--panel); }
		button.primary { border-color: var(--red); background: var(--red); color: #fff; }
		button.paid { border-color: var(--green); background: var(--green); color: #fff; }
		button:disabled { opacity: .5; cursor: not-allowed; }
		#metrics { color: #555; font-size: .875rem; }
		.empty { padding: 24px; text-align: center; background: var(--panel); }
	</style>
</head>
<body>
	<main>
		<header><h1>Local Payment Simulator</h1><span id="connection">Connecting...</span></header>
		<p class="notice">Payments are simulated locally. No real payment provider is contacted. Use these controls to advance each device-created payment.</p>
		<div id="metrics"></div>
		<section id="payments" aria-live="polite"></section>
	</main>
	<script>
	const focusedPayment = new URLSearchParams(location.search).get('payment');
	const paymentsEl = document.getElementById('payments');
	const connectionEl = document.getElementById('connection');
	const metricsEl = document.getElementById('metrics');
	function button(label, onClick, className) {
		const element = document.createElement('button');
		element.textContent = label;
		if (className) element.className = className;
		element.onclick = onClick;
		return element;
	}
	async function action(id, name, body) {
		const response = await fetch('/dev/payment/' + encodeURIComponent(id) + '/' + name, {
			method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {})
		});
		if (!response.ok) {
			const result = await response.json();
			alert(result.error || 'Could not update payment');
		}
		refresh();
	}
	function renderPayment(payment) {
		const card = document.createElement('article');
		card.className = 'payment';
		card.id = 'payment-' + payment.id;
		const title = document.createElement('h2');
		title.textContent = payment.id + ' · ' + payment.status;
		const meta = document.createElement('p');
		const amount = payment.amount_cents == null ? 'not selected' : (payment.amount_cents / 100).toFixed(2) + ' CHF';
		meta.className = 'meta';
		meta.textContent = 'Phase: ' + payment.payment_phase + ' · Amount: ' + amount;
		const actions = document.createElement('div');
		actions.className = 'actions';
		if (payment.status === 'waiting' && payment.payment_phase === 'waiting_for_amount') {
			if (!payment.qr_scanned) {
				actions.appendChild(button('QR Scanned', () => action(payment.id, 'scanned'), 'primary'));
			} else {
				for (const cents of [500, 1000, 2000, 5000]) {
					actions.appendChild(button('Choose ' + (cents / 100) + ' CHF', () => action(payment.id, 'amount', { amount_cents: cents }), 'primary'));
				}
			}
		}
		if (payment.status === 'waiting' && payment.payment_phase === 'waiting_for_payment') {
			actions.appendChild(button('Simulate successful payment', () => action(payment.id, 'paid'), 'paid'));
			actions.appendChild(button('Fail payment', () => action(payment.id, 'fail')));
			actions.appendChild(button('Cancel payment', () => action(payment.id, 'cancel')));
		}
		card.append(title, meta, actions);
		return card;
	}
	async function refresh() {
		try {
			const response = await fetch('/dev/state', { cache: 'no-store' });
			if (!response.ok) throw new Error('Mock API unavailable');
			const state = await response.json();
			connectionEl.textContent = 'Mock API ready';
			metricsEl.textContent = state.status_reports + ' device status reports · ' + state.log_lines + ' local log lines received';
			paymentsEl.replaceChildren();
			if (!state.payments.length) {
				const empty = document.createElement('div');
				empty.className = 'empty';
				empty.textContent = 'Waiting for the device to create a payment.';
				paymentsEl.appendChild(empty);
			} else {
				for (const payment of state.payments) paymentsEl.appendChild(renderPayment(payment));
			}
			if (focusedPayment) document.getElementById('payment-' + focusedPayment)?.scrollIntoView({ block: 'center' });
		} catch {
			connectionEl.textContent = 'Mock API offline';
		}
	}
	refresh();
	setInterval(refresh, 1000);
	</script>
</body>
</html>`
