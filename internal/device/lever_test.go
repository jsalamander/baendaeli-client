package device

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jsalamander/baendaeli-client/internal/config"
)

func TestLeverLogsIgnoredBusyStatePress(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)

	c := newLeverTestClient(t)
	request, _ := c.currentLeverRequest()
	c.setRuntimeState(StateCommandExecuting, "busy")
	c.handleLeverRequest(request)
	text := output.String()
	for _, expected := range []string{"lever press ignored", "state=command_executing", "requires ball_detected or awaiting_payment"} {
		if !strings.Contains(text, expected) {
			t.Errorf("expected %q in ignored press log: %s", expected, text)
		}
	}
}

type stubLeverSwitch struct {
	enabled    bool
	active     atomic.Bool
	readFailed atomic.Bool
	initErr    error
	initCalls  atomic.Int32
	closeCalls atomic.Int32
}

func (s *stubLeverSwitch) IsEnabled() bool { return s.enabled }
func (s *stubLeverSwitch) Init(_ *config.Config) error {
	s.initCalls.Add(1)
	return s.initErr
}
func (s *stubLeverSwitch) ReadActive() (bool, error) {
	if s.readFailed.Load() {
		return false, errors.New("lever read failed")
	}
	return s.active.Load(), nil
}
func (s *stubLeverSwitch) Close() error {
	s.closeCalls.Add(1)
	return nil
}

func newLeverTestClient(t *testing.T) *Client {
	t.Helper()
	c := New(&config.Config{LeverEnabled: true, DebugBypassBallDetection: true})
	t.Cleanup(c.cancel)
	c.printer = &stubTicketPrinter{}
	c.triggerActuator = func() (int, error) { return 1, nil }
	c.setCurrentPayment("pending-payment", map[string]any{
		"id":            "pending-payment",
		"status":        "pending",
		"payment_phase": "waiting_for_payment",
		"qr_code_url":   "https://example.com/qr/pending-payment",
	})
	c.setRuntimeState(StateAwaitingPayment, "Warten auf Zahlung")
	return c
}

func TestLeverDebouncer(t *testing.T) {
	start := time.Now()
	var d leverDebouncer
	for _, tc := range []struct {
		atMs   int
		active bool
		fire   bool
	}{
		{0, true, false}, // Held on startup must not trigger.
		{100, true, false},
		{110, false, false},
		{159, false, false},
		{160, false, false},
		{170, true, false},
		{190, false, false}, // Contact bounce.
		{200, true, false},
		{249, true, false},
		{250, true, true},
		{500, true, false},
		{510, false, false},
		{530, true, false}, // A short release must not re-arm.
		{600, true, false},
		{610, false, false},
		{660, false, false},
		{670, true, false},
		{720, true, true},
	} {
		if fire := d.update(tc.active, start.Add(time.Duration(tc.atMs)*time.Millisecond)); fire != tc.fire {
			t.Fatalf("at %d ms, active=%t: fired=%t, want %t", tc.atMs, tc.active, fire, tc.fire)
		}
	}
}

func TestLeverFreeDispenseRetainsPaymentAndDoesNotCountAsPaid(t *testing.T) {
	c := newLeverTestClient(t)
	var triggers int
	c.triggerActuator = func() (int, error) {
		triggers++
		snapshot := c.GetStateSnapshot()
		if snapshot.State != string(StateDispensing) || snapshot.PaymentID != "pending-payment" {
			t.Errorf("unexpected free-dispense snapshot: %+v", snapshot)
		}
		return 1, nil
	}
	request, eligible := c.currentLeverRequest()
	if !eligible {
		t.Fatal("awaiting_payment must accept a lever request")
	}
	c.handleLeverRequest(request)
	c.handleLeverRequest(request)

	if triggers != 1 {
		t.Fatalf("a request must dispense once, got %d", triggers)
	}
	snapshot := c.GetStateSnapshot()
	if snapshot.State != string(StateAwaitingPayment) || snapshot.PaymentID != request.paymentID {
		t.Fatalf("expected same pending payment after free dispense: %+v", snapshot)
	}
	if snapshot.Payment["qr_code_url"] != "https://example.com/qr/pending-payment" {
		t.Fatal("pending payment QR must be retained")
	}
	if snapshot.Payment["status"] != "pending" {
		t.Fatal("free dispense must not mark the payment as paid")
	}
	if c.pendingDispensedCount(request.paymentID) != nil {
		t.Fatal("free dispense must not record a payment-linked dispense")
	}
	if c.printer.(*stubTicketPrinter).paymentCalls.Load() != 0 {
		t.Fatal("free dispense must not print a payment receipt")
	}
}

func TestLeverQRScreenDispensesAndRestoresSameQR(t *testing.T) {
	c := newLeverTestClient(t)
	c.setCurrentPayment("pending-payment", map[string]any{
		"payment_phase": "waiting_for_amount",
	})
	c.setRuntimeState(StateBallDetected, "Bitte QR-Code scannen und zahlen")
	triggers := 0
	c.triggerActuator = func() (int, error) {
		triggers++
		if c.GetStateSnapshot().State != string(StateDispensing) {
			t.Error("free dispense must expose dispensing state")
		}
		return 1, nil
	}
	for i := 0; i < 2; i++ {
		request, eligible := c.currentLeverRequest()
		if !eligible {
			t.Fatal("QR screen must accept a fresh lever request before scanning")
		}
		c.handleLeverRequest(request)
		c.handleLeverRequest(request)
		snapshot := c.GetStateSnapshot()
		if snapshot.State != string(StateBallDetected) || snapshot.ExecutingCommand != nil {
			t.Fatalf("must restore QR screen without an overlay: %+v", snapshot)
		}
		if snapshot.PaymentID != "pending-payment" ||
			snapshot.Payment["qr_code_url"] != "https://example.com/qr/pending-payment" ||
			snapshot.Payment["payment_phase"] != "waiting_for_amount" {
			t.Fatalf("must retain the same unscanned payment and QR: %+v", snapshot)
		}
		if c.pendingDispensedCount("pending-payment") != nil {
			t.Fatal("free ball must not consume the pending payment")
		}
	}
	if triggers != 2 || c.printer.(*stubTicketPrinter).paymentCalls.Load() != 0 {
		t.Fatalf("expected two free dispenses without receipts, got triggers=%d", triggers)
	}
}

func TestLeverRetainedPaymentCanDispenseWhenPaid(t *testing.T) {
	var reports atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/payment/pending-payment":
			w.Write([]byte(`{"status":"paid","payment_phase":"waiting_for_payment","amount_cents":2500}`))
		case "/api/v1/device/status":
			reports.Add(1)
			w.Write([]byte(`{"success":true}`))
		default:
			t.Errorf("unexpected API request after free dispense: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c := newLeverTestClient(t)
	c.config.BaendaeliURL = server.URL
	triggers := 0
	c.triggerActuator = func() (int, error) { triggers++; return 1, nil }
	request, _ := c.currentLeverRequest()
	c.handleLeverRequest(request)
	if reports.Load() != 0 {
		t.Fatal("free dispense must not report a paid dispense")
	}
	c.runStateMachineCycle()
	if triggers != 2 {
		t.Fatalf("retained paid payment must dispense the next ball, total triggers=%d", triggers)
	}
	if reports.Load() != 1 || c.GetPaymentID() != "" {
		t.Fatal("normal paid flow must report the dispense and clear the retained payment")
	}
	if c.printer.(*stubTicketPrinter).paymentCalls.Load() != 1 {
		t.Fatal("only the subsequent paid dispense should print a receipt")
	}
}

func TestLeverIgnoredOutsideAwaitingPayment(t *testing.T) {
	for _, state := range []RuntimeState{
		StateStarting, StateStartupCycle, StateDetectingBall, StateBallOnSensor,
		StateBallStuckFunnel, StateDispensing, StatePaymentFailed,
		StateJam, StateIdle, StateCommandExecuting, StateError, StateSoldOut,
	} {
		t.Run(string(state), func(t *testing.T) {
			c := newLeverTestClient(t)
			request, _ := c.currentLeverRequest()
			c.setRuntimeState(state, "not ready for lever")
			c.triggerActuator = func() (int, error) { t.Error("unexpected dispense"); return 0, nil }
			if _, eligible := c.currentLeverRequest(); eligible {
				t.Fatal("lever must be ineligible in this state")
			}
			c.handleLeverRequest(request)
			if c.GetStateSnapshot().State != string(state) {
				t.Fatal("ignored lever request changed runtime state")
			}
		})
	}
}

func TestLeverRejectsStaleAndIneligibleRequests(t *testing.T) {
	for _, name := range []string{"disabled", "jammed", "payment replaced", "payment cleared", "already dispensed", "state changed back", "stopped"} {
		t.Run(name, func(t *testing.T) {
			c := newLeverTestClient(t)
			request, _ := c.currentLeverRequest()
			switch name {
			case "disabled":
				c.config.LeverEnabled = false
			case "jammed":
				c.jammed.Store(true)
			case "payment replaced":
				c.SetPaymentID("replacement")
			case "payment cleared":
				c.SetPaymentID("")
			case "already dispensed":
				c.recordDispensedCount(request.paymentID, 1)
			case "state changed back":
				c.setRuntimeState(StateCommandExecuting, "busy")
				c.setRuntimeState(StateAwaitingPayment, "waiting again")
			case "stopped":
				c.cancel()
			}
			c.triggerActuator = func() (int, error) { t.Error("stale request dispensed"); return 0, nil }
			c.handleLeverRequest(request)
		})
	}
}

func TestLeverDispenseFailureKeepsPendingPayment(t *testing.T) {
	c := newLeverTestClient(t)
	c.triggerActuator = func() (int, error) { return 0, errors.New("actuator failed") }
	request, _ := c.currentLeverRequest()
	c.handleLeverRequest(request)
	snapshot := c.GetStateSnapshot()
	if snapshot.State != string(StateError) || snapshot.Message != "Gratis-Ausgabe fehlgeschlagen" {
		t.Fatalf("dispense failure must be surfaced: %+v", snapshot)
	}
	if snapshot.PaymentID != request.paymentID || c.pendingDispensedCount(request.paymentID) != nil {
		t.Fatal("failed free dispense must leave the pending payment untouched")
	}
}

func TestLeverPostDispenseJamIsSurfaced(t *testing.T) {
	cfg := &config.Config{LeverEnabled: true}
	cfg.SetDefaults()
	cfg.ColorSensorMovementThreshold = 10000
	cfg.ColorSensorCheckDurationMs = 1
	cfg.ColorSensorVibrateBursts = 0
	cfg.ColorSensorMaxAttempts = 1
	c := New(cfg)
	t.Cleanup(c.cancel)
	if err := c.colorSensor.Init(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.colorSensor.Close(); err != nil {
			t.Error(err)
		}
	})
	c.triggerActuator = func() (int, error) { return 1, nil }
	c.SetPaymentID("pending-payment")
	c.setRuntimeState(StateAwaitingPayment, "waiting")
	request, _ := c.currentLeverRequest()
	c.handleLeverRequest(request)
	snapshot := c.GetStateSnapshot()
	if !snapshot.Jammed || snapshot.State != string(StateBallStuckFunnel) || snapshot.ExecutingCommand == nil {
		t.Fatalf("post-dispense jam must remain visible: %+v", snapshot)
	}
	if snapshot.PaymentID != request.paymentID || c.pendingDispensedCount(request.paymentID) != nil {
		t.Fatal("post-dispense jam must not consume the pending payment")
	}
}

func TestLeverMonitorRequiresNewMovementInEligibleState(t *testing.T) {
	c := newLeverTestClient(t)
	sw := &stubLeverSwitch{enabled: true}
	c.leverSwitch = sw
	c.wg.Add(1)
	go c.monitorLever()
	t.Cleanup(func() { c.cancel(); c.wg.Wait() })

	time.Sleep(2 * leverDebounce)
	sw.active.Store(true)
	select {
	case request := <-c.leverRequests:
		if request.paymentID != "pending-payment" {
			t.Fatal("request must reference current payment")
		}
	case <-time.After(time.Second):
		t.Fatal("eligible lever movement did not queue a request")
	}
	if c.GetStateSnapshot().State != string(StateAwaitingPayment) {
		t.Fatal("GPIO monitor must not execute device actions")
	}
	assertNoRequest := func() {
		t.Helper()
		select {
		case <-c.leverRequests:
			t.Fatal("unexpected repeated or ineligible lever request")
		case <-time.After(2 * leverDebounce):
		}
	}
	assertNoRequest()
	c.setRuntimeState(StateCommandExecuting, "busy")
	sw.active.Store(false)
	time.Sleep(2 * leverDebounce)
	sw.active.Store(true)
	assertNoRequest()
	c.setRuntimeState(StateAwaitingPayment, "waiting")
	assertNoRequest()
	sw.active.Store(false)
	time.Sleep(2 * leverDebounce)
	sw.active.Store(true)
	select {
	case <-c.leverRequests:
	case <-time.After(time.Second):
		t.Fatal("neutral followed by a new press must re-arm")
	}
}

func TestLeverWakesDeviceLoopWithoutWaitingForPollTick(t *testing.T) {
	c := newLeverTestClient(t)
	c.pollInterval = time.Hour
	triggered := make(chan struct{}, 1)
	c.triggerActuator = func() (int, error) { triggered <- struct{}{}; return 1, nil }
	c.wg.Add(1)
	go c.pollLoop()
	t.Cleanup(func() { c.cancel(); c.wg.Wait() })
	request, _ := c.currentLeverRequest()
	c.leverRequests <- request
	select {
	case <-triggered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("lever request must wake the loop immediately, not wait for an API poll tick")
	}
}

func TestLeverLifecycle(t *testing.T) {
	for _, name := range []string{"enabled", "disabled", "init failure", "read failure"} {
		t.Run(name, func(t *testing.T) {
			c := newLeverTestClient(t)
			c.pollInterval = time.Hour
			sw := &stubLeverSwitch{enabled: name != "disabled"}
			if name == "init failure" {
				sw.initErr = errors.New("GPIO unavailable")
			}
			if name == "read failure" {
				sw.readFailed.Store(true)
			}
			c.config.LeverEnabled = sw.enabled
			c.leverSwitch = sw
			c.Start()
			time.Sleep(2 * leverPollInterval)
			c.Stop()
			wantInit := int32(1)
			if name == "disabled" {
				wantInit = 0
			}
			if sw.initCalls.Load() != wantInit || sw.closeCalls.Load() != 1 {
				t.Fatalf("unexpected lifecycle calls: init=%d close=%d", sw.initCalls.Load(), sw.closeCalls.Load())
			}
			if len(c.leverRequests) != 0 {
				t.Fatal("disabled or unavailable hardware must not queue a dispense")
			}
		})
	}
}
