package device

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jsalamander/baendaeli-client/internal/actuator"
	"github.com/jsalamander/baendaeli-client/internal/breakbeam"
	"github.com/jsalamander/baendaeli-client/internal/camera"
	"github.com/jsalamander/baendaeli-client/internal/colorsensor"
	"github.com/jsalamander/baendaeli-client/internal/config"
	"github.com/jsalamander/baendaeli-client/internal/version"
	"github.com/jsalamander/baendaeli-client/internal/vibrator"
)

// StatusRequest is sent to the server
type StatusRequest struct {
	PaymentID      *string `json:"payment_id,omitempty"`
	ClientVersion  string  `json:"client_version"`
	DispensedCount *int    `json:"dispensed_count,omitempty"`
}

// StatusResponse is received from the server
type StatusResponse struct {
	Success bool `json:"success"`
}

// CommandResponse is received from the server
type CommandResponse struct {
	ID          int    `json:"id"`
	Command     string `json:"command"`
	DurationMs  *int   `json:"duration_ms,omitempty"`  // Optional duration in milliseconds
	RepeatCount *int   `json:"repeat_count,omitempty"` // Optional repeat count for load_test cycles
	Message     string `json:"message,omitempty"`      // Message text for message command
	Percent     *int   `json:"percent,omitempty"`      // Vibration intensity (1-100) for vibrate command
}

// AckRequest is sent to the server
type AckRequest struct {
	Status       string `json:"status"`                  // "success" or "failed"
	ErrorMessage string `json:"error_message,omitempty"` // max 1000 chars, only for failed status
	ImageBase64  string `json:"image_base64,omitempty"`  // base64-encoded JPEG, required on success for take_picture
}

// AckResponse is received from the server
type AckResponse struct {
	Success bool `json:"success"`
}

type pendingDispense struct {
	paymentID string
	count     int
}

type paymentCreateRequest struct {
	Currency           string `json:"currency"`
	PaymentRedirectURL string `json:"payment_redirect_url"`
}

type paymentCreateResponse struct {
	ID string `json:"id"`
}

type paymentStatusResponse struct {
	Status string `json:"status"`
}

type RuntimeState string

const (
	StateStarting         RuntimeState = "starting"
	StateStartupCycle     RuntimeState = "startup_cycle"
	StateDetectingBall    RuntimeState = "detecting_ball"
	StateBallOnSensor     RuntimeState = "ball_on_sensor"
	StateBallDetected     RuntimeState = "ball_detected"
	StateBallStuckFunnel  RuntimeState = "ball_stuck_in_funnel"
	StateAwaitingPayment  RuntimeState = "awaiting_payment"
	StateDispensing       RuntimeState = "dispensing"
	StatePaymentFailed    RuntimeState = "payment_failed"
	StateJam              RuntimeState = "jam"
	StateIdle             RuntimeState = "idle"
	StateCommandExecuting RuntimeState = "command_executing"
	StateError            RuntimeState = "error"
)

type StateSnapshot struct {
	State            string           `json:"state"`
	Message          string           `json:"message,omitempty"`
	PaymentID        string           `json:"payment_id,omitempty"`
	Payment          map[string]any   `json:"payment,omitempty"`
	Jammed           bool             `json:"jammed"`
	ExecutingCommand *CommandResponse `json:"executing_command,omitempty"`
	PendingCommand   *CommandResponse `json:"pending_command,omitempty"`
}

type breakBeamSensor interface {
	IsEnabled() bool
	ReadInterrupted() (bool, error)
	Init(cfg *config.Config) error
	Close() error
}

// Client polls the device API and executes commands
type Client struct {
	config           *config.Config
	httpClient       *http.Client
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	pollInterval     time.Duration
	paymentIDMutex   sync.Mutex
	currentPaymentID string
	currentPayment   map[string]any
	running          atomic.Bool
	colorSensor      *colorsensor.Sensor
	breakBeamSensor  breakBeamSensor
	jammed           atomic.Bool

	// Command execution status
	statusMutex      sync.Mutex
	executingCommand *CommandResponse
	pendingCommand   *CommandResponse
	pendingBallRef   *uint16
	state            RuntimeState
	stateMessage     string
	lastCommandError string // Error from last command execution
	dispenseMutex    sync.Mutex
	pendingDispense  *pendingDispense
	logShipper       *logShipper

	// Actuator lock to prevent concurrent commands
	actuatorMutex sync.Mutex
}

// New creates a new device client
func New(cfg *config.Config) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		config:          cfg,
		httpClient:      &http.Client{Timeout: 15 * time.Second},
		ctx:             ctx,
		cancel:          cancel,
		pollInterval:    7 * time.Second,
		colorSensor:     colorsensor.New(cfg),
		breakBeamSensor: breakbeam.New(cfg),
		state:           StateStarting,
	}
	c.logShipper = newLogShipper(ctx, c, c.httpClient, io.Discard)
	return c
}

// SetLogShippingDiagnosticsWriter configures where shipper diagnostics are written.
func (c *Client) SetLogShippingDiagnosticsWriter(w io.Writer) {
	if c.logShipper == nil {
		return
	}
	if w == nil {
		w = io.Discard
	}
	c.logShipper.diagWriter = w
}

// LogSinkWriter returns an io.Writer that enqueues lines for API shipping.
func (c *Client) LogSinkWriter() io.Writer {
	return logSinkWriter{shipper: c.logShipper}
}

// SetPaymentID updates the current payment ID
func (c *Client) SetPaymentID(paymentID string) {
	c.paymentIDMutex.Lock()
	defer c.paymentIDMutex.Unlock()
	c.currentPaymentID = paymentID
	if paymentID == "" {
		c.currentPayment = nil
	}
	c.dropStalePendingDispense(paymentID)
}

// GetPaymentID returns the current payment ID
func (c *Client) GetPaymentID() string {
	c.paymentIDMutex.Lock()
	defer c.paymentIDMutex.Unlock()
	return c.currentPaymentID
}

func (c *Client) setCurrentPayment(paymentID string, payment map[string]any) {
	c.paymentIDMutex.Lock()
	defer c.paymentIDMutex.Unlock()
	c.currentPaymentID = paymentID
	merged := cloneMap(c.currentPayment)
	if merged == nil {
		merged = make(map[string]any)
	}
	for key, value := range payment {
		merged[key] = value
	}
	c.currentPayment = merged
	c.dropStalePendingDispense(paymentID)
}

func (c *Client) getCurrentPayment() map[string]any {
	c.paymentIDMutex.Lock()
	defer c.paymentIDMutex.Unlock()
	return cloneMap(c.currentPayment)
}

// GetExecutingCommand returns the currently executing command, if any
func (c *Client) GetExecutingCommand() *CommandResponse {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	return c.executingCommand
}

func (c *Client) GetStateSnapshot() StateSnapshot {
	c.statusMutex.Lock()
	state := c.state
	message := c.stateMessage
	var cmdCopy *CommandResponse
	var pendingCopy *CommandResponse
	if c.executingCommand != nil {
		copyValue := *c.executingCommand
		cmdCopy = &copyValue
	}
	if c.pendingCommand != nil {
		copyValue := *c.pendingCommand
		pendingCopy = &copyValue
	}
	c.statusMutex.Unlock()

	return StateSnapshot{
		State:            string(state),
		Message:          message,
		PaymentID:        c.GetPaymentID(),
		Payment:          c.getCurrentPayment(),
		Jammed:           c.jammed.Load(),
		ExecutingCommand: cmdCopy,
		PendingCommand:   pendingCopy,
	}
}

// setExecutingCommand sets the currently executing command
func (c *Client) setExecutingCommand(cmd *CommandResponse) {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	c.executingCommand = cmd
}

func (c *Client) setRuntimeState(state RuntimeState, message string) {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	c.state = state
	c.stateMessage = message
}

func (c *Client) updateExecutingCommandMessage(message string) {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	if c.executingCommand == nil {
		return
	}
	c.executingCommand.Message = message
}

// clearExecutingCommand clears the executing command
func (c *Client) clearExecutingCommand() {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	c.executingCommand = nil
}

func (c *Client) getPendingCommand() *CommandResponse {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	if c.pendingCommand == nil {
		return nil
	}
	copyValue := *c.pendingCommand
	return &copyValue
}

func (c *Client) setPendingCommand(cmd *CommandResponse) {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	if cmd == nil {
		c.pendingCommand = nil
		return
	}
	copyValue := *cmd
	c.pendingCommand = &copyValue
}

func (c *Client) clearPendingCommand() {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	c.pendingCommand = nil
}

func (c *Client) setPendingBallReference(baseline *uint16) {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	if baseline == nil {
		c.pendingBallRef = nil
		return
	}
	copyValue := *baseline
	c.pendingBallRef = &copyValue
}

func (c *Client) consumePendingBallReference() *uint16 {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	if c.pendingBallRef == nil {
		return nil
	}
	copyValue := *c.pendingBallRef
	c.pendingBallRef = nil
	return &copyValue
}

// LockActuator acquires the actuator lock
// Must be paired with UnlockActuator()
func (c *Client) LockActuator() {
	c.actuatorMutex.Lock()
}

// UnlockActuator releases the actuator lock
func (c *Client) UnlockActuator() {
	c.actuatorMutex.Unlock()
}

// Start begins the polling loop
func (c *Client) Start() {
	if !c.running.CompareAndSwap(false, true) {
		log.Println("Device client is already running")
		return
	}

	if c.config.ActuatorEnabled {
		log.Println("Device client: homing actuator before startup ball check")
		c.setRuntimeState(StateStarting, "Homing actuator")
		actuator.Home()
	}

	if err := c.colorSensor.Init(c.config); err != nil {
		log.Printf("Device client: colour sensor init failed: %v", err)
	}
	if err := c.breakBeamSensor.Init(c.config); err != nil {
		log.Printf("Device client: break-beam sensor init failed: %v", err)
	}

	if c.config.ActuatorEnabled {
		if err := c.runStartupExtractorCycle(); err != nil {
			c.setRuntimeState(StateError, "Startup cycle failed")
			log.Printf("Device client: startup extractor cycle failed: %v", err)
		}
	}

	c.setRuntimeState(StateDetectingBall, "Warte auf Ball")

	if c.logShipper != nil {
		c.logShipper.start()
	}

	c.wg.Add(1)
	go c.pollLoop()
	log.Println("Device client started")
}

// Stop gracefully shuts down the device client
func (c *Client) Stop() {
	if !c.running.CompareAndSwap(true, false) {
		return
	}
	c.cancel()
	c.wg.Wait()
	if c.logShipper != nil {
		c.logShipper.stopAndFlush(3 * time.Second)
	}
	if err := c.colorSensor.Close(); err != nil {
		log.Printf("Device client: failed to close colour sensor: %v", err)
	}
	if err := c.breakBeamSensor.Close(); err != nil {
		log.Printf("Device client: failed to close break-beam sensor: %v", err)
	}
	log.Println("Device client stopped")
}

type logSinkWriter struct {
	shipper *logShipper
}

func (w logSinkWriter) Write(p []byte) (int, error) {
	if w.shipper == nil {
		return len(p), nil
	}
	for _, line := range strings.Split(string(p), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		w.shipper.enqueue(line)
	}
	return len(p), nil
}

// pollLoop runs the main polling loop
func (c *Client) pollLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.poll()
		}
	}
}

// poll performs one iteration of the polling cycle
func (c *Client) poll() {
	// 1. Report status
	paymentID := c.GetPaymentID()
	if err := c.reportStatus(paymentID); err != nil {
		log.Printf("Device client: failed to report status: %v", err)
	}

	// If a jam was detected, keep showing the message and skip command execution.
	if c.jammed.Load() {
		referenceBaseline := c.consumePendingBallReference()
		if err := c.waitForBallReady(false, false, referenceBaseline); err != nil {
			c.setRuntimeState(StateJam, "Stau detektiert")
		} else {
			// Jam cleared, continue normal polling and command handling.
			c.setRuntimeState(StateDetectingBall, "Stau behoben")
			log.Printf("Device client: jam cleared by passive ball detection")
		}
	}

	// While jam is active we still allow command polling (e.g. restart), but skip
	// autonomous state-machine actions until jam is cleared.
	if !c.jammed.Load() {
		c.runStateMachineCycle()
	}

	// 2. Get next command (or keep a previously deferred command)
	cmd := c.getPendingCommand()
	var err error
	if cmd == nil {
		cmd, err = c.getCommand()
	}
	if err != nil {
		log.Printf("Device client: failed to get command: %v", err)
		return
	}

	// 3. If command is not null, execute it
	if cmd != nil && cmd.Command != "" {
		if !c.canExecuteCommandNow(cmd) {
			c.setPendingCommand(cmd)
			return
		}

		c.clearPendingCommand()
		c.setRuntimeState(StateCommandExecuting, "Operator-Befehl wird ausgefuhrt")
		imageData, execErr := c.executeCommand(cmd)
		if execErr != nil {
			c.setRuntimeState(StateError, execErr.Error())
			log.Printf("Device client: failed to execute command %d (%s): %v", cmd.ID, cmd.Command, execErr)
		}

		// 4. Acknowledge the command with success/failure status
		if err := c.ackCommand(cmd.ID, execErr, imageData); err != nil {
			log.Printf("Device client: failed to acknowledge command %d: %v", cmd.ID, err)
		}

		c.clearExecutingCommand()
		if c.jammed.Load() {
			c.setRuntimeState(StateJam, "Stau detektiert")
			return
		}
		if c.GetPaymentID() == "" {
			c.setRuntimeState(StateDetectingBall, "Warte auf Ball")
		}
	}
}

// runStateMachineCycle returns true when the state-driven flow handled this cycle.
func (c *Client) runStateMachineCycle() bool {
	paymentID := c.GetPaymentID()
	if paymentID == "" {
		c.setRuntimeState(StateDetectingBall, "Warte auf Ball")
		referenceBaseline := c.consumePendingBallReference()
		if err := c.waitForBallReady(true, true, referenceBaseline); err != nil {
			log.Printf("Device client: ball detection failed: %v", err)
			return true
		}
		c.setRuntimeState(StateBallOnSensor, "Ball auf Sensor erkannt")
		if _, err := c.createPayment(); err != nil {
			c.setRuntimeState(StateError, "Payment konnte nicht erstellt werden")
			log.Printf("Device client: failed to create payment after ball detection: %v", err)
			return false
		}
		c.setRuntimeState(StateBallDetected, "Bitte QR-Code scannen und Betrag wählen")
		return true
	}

	status, payment, err := c.getPaymentStatus(paymentID)
	if err != nil {
		c.setRuntimeState(StateError, "Payment-Status nicht verfugbar")
		log.Printf("Device client: failed to fetch payment status for %s: %v", paymentID, err)
		return true
	}
	c.setCurrentPayment(paymentID, payment)

	switch status {
	case "waiting", "pending", "open":
		if paymentPhase(payment) == "waiting_for_payment" {
			c.setRuntimeState(StateAwaitingPayment, "Warten auf Zahlung")
			c.setExecutingCommand(&CommandResponse{
				Command: "message",
				Message: "Warten auf Zahlung",
			})
			return true
		}
		c.setRuntimeState(StateBallDetected, "Bitte QR-Code scannen und Betrag wählen")
		c.clearExecutingCommand()
		return true
	case "success", "paid", "completed":
		c.setRuntimeState(StateDispensing, "Ausgabe laeuft")
		c.setExecutingCommand(&CommandResponse{
			Command: "message",
			Message: "Zahlung erhalten - Ausgabe läuft",
		})

		// If this payment was already dispensed in a prior cycle, do not dispense again.
		// Just keep retrying status report until the dispense count is successfully sent.
		if pending := c.pendingDispensedCount(paymentID); pending == nil {
			if _, err := c.DispenseAndWaitForBall(); err != nil {
				c.setRuntimeState(StateError, "Ausgabe fehlgeschlagen")
				log.Printf("Device client: dispense failed after successful payment: %v", err)
				return true
			}
		} else if c.config.BreakBeamDebugLogging {
			log.Printf("Device client: payment %s already dispensed (pending_count=%d), skipping repeated dispense", paymentID, *pending)
		}

		// Ensure the non-zero dispensed_count is reported before clearing the payment.
		if err := c.reportStatus(paymentID); err != nil {
			c.setRuntimeState(StateError, "Dispense-Status konnte nicht gemeldet werden")
			log.Printf("Device client: failed to report dispensed count for payment %s: %v", paymentID, err)
			return true
		}

		c.SetPaymentID("")
		c.clearExecutingCommand()
		c.setRuntimeState(StateDetectingBall, "Warte auf Ball")
		return true
	case "failure", "failed", "cancelled", "canceled", "expired", "timeout":
		c.setRuntimeState(StatePaymentFailed, "Zahlung fehlgeschlagen")
		c.setExecutingCommand(&CommandResponse{
			Command: "message",
			Message: "Zahlung abgebrochen - zurückgesetzt",
		})
		c.SetPaymentID("")
		time.Sleep(500 * time.Millisecond)
		c.clearExecutingCommand()
		c.setRuntimeState(StateDetectingBall, "Warte auf Ball")
		return true
	default:
		c.setRuntimeState(StateError, "Unbekannter Payment-Status")
		log.Printf("Device client: unknown payment status %q for payment %s", status, paymentID)
		return true
	}
}

// reportStatus sends the current payment ID to the server
func (c *Client) reportStatus(paymentID string) error {
	url := c.buildURL("/api/v1/device/status")
	dispensedCount := c.pendingDispensedCount(paymentID)
	if dispensedCount == nil {
		// Always send dispensed_count: backend requires the field.
		// 0 is correct before a dispense or when idle.
		zero := 0
		dispensedCount = &zero
	}
	var requestPaymentID *string
	if paymentID != "" {
		requestPaymentID = &paymentID
	}
	req := StatusRequest{
		PaymentID:      requestPaymentID,
		ClientVersion:  version.AppVersion,
		DispensedCount: dispensedCount,
	}

	paymentLabel := "<none>"
	if requestPaymentID != nil {
		paymentLabel = *requestPaymentID
	}
	log.Printf("Device client: reporting status payment_id=%s dispensed_count=%d", paymentLabel, *dispensedCount)

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal status request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(c.ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeader(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized: invalid or missing API key")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var statusResp StatusResponse
	if err := decodeJSONResponse(respBody, &statusResp, resp.Header.Get("Content-Type")); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !statusResp.Success {
		return fmt.Errorf("server returned success=false")
	}

	// Do NOT clear pendingDispense after a successful ack.
	// The count must remain at its confirmed value (e.g. 1) for all subsequent
	// polls of the same payment_id. Sending 0 again would overwrite the server
	// record. The count is reset only when the payment_id changes (handled by
	// dropStalePendingDispense).

	return nil
}

func (c *Client) pendingDispensedCount(paymentID string) *int {
	if paymentID == "" {
		return nil
	}

	c.dispenseMutex.Lock()
	defer c.dispenseMutex.Unlock()

	if c.pendingDispense == nil || c.pendingDispense.paymentID != paymentID {
		return nil
	}

	count := c.pendingDispense.count
	return &count
}

func (c *Client) recordDispensedCount(paymentID string, count int) {
	if paymentID == "" || count < 0 {
		return
	}

	c.dispenseMutex.Lock()
	defer c.dispenseMutex.Unlock()

	if c.pendingDispense != nil && c.pendingDispense.paymentID == paymentID {
		c.pendingDispense.count += count
		return
	}

	c.pendingDispense = &pendingDispense{paymentID: paymentID, count: count}
}

func (c *Client) clearPendingDispense(paymentID string, dispensedCount *int) {
	if dispensedCount == nil {
		return
	}

	c.dispenseMutex.Lock()
	defer c.dispenseMutex.Unlock()

	if c.pendingDispense == nil {
		return
	}
	if c.pendingDispense.paymentID != paymentID {
		return
	}
	if c.pendingDispense.count != *dispensedCount {
		return
	}

	c.pendingDispense = nil
}

func (c *Client) dropStalePendingDispense(paymentID string) {
	c.dispenseMutex.Lock()
	defer c.dispenseMutex.Unlock()

	if c.pendingDispense != nil && c.pendingDispense.paymentID != paymentID {
		c.pendingDispense = nil
	}
}

// getCommand fetches the next pending command from the server
func (c *Client) getCommand() (*CommandResponse, error) {
	url := c.buildURL("/api/v1/device/commands")

	httpReq, err := http.NewRequestWithContext(c.ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeader(httpReq)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("unauthorized: invalid or missing API key")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var cmdResp CommandResponse
	if err := decodeJSONResponse(respBody, &cmdResp, resp.Header.Get("Content-Type")); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// If command is empty/null, return nil
	if cmdResp.Command == "" {
		return nil, nil
	}

	return &cmdResp, nil
}

func (c *Client) createPayment() (string, error) {
	url := c.buildURL("/api/v1/payment")
	c.setRuntimeState(StateBallDetected, "Erstelle Zahlung")

	req := paymentCreateRequest{
		Currency:           "CHF",
		PaymentRedirectURL: "https://example.com/payments/complete",
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payment request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(c.ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeader(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("unauthorized: invalid or missing API key")
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var payment map[string]any
	if err := decodeJSONResponse(respBody, &payment, resp.Header.Get("Content-Type")); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	id, _ := payment["id"].(string)
	paymentResp := paymentCreateResponse{ID: id}

	if paymentResp.ID == "" {
		return "", fmt.Errorf("payment API response missing id")
	}

	c.setCurrentPayment(paymentResp.ID, payment)
	log.Printf("Device client: created payment %s", paymentResp.ID)
	return paymentResp.ID, nil
}

func (c *Client) getPaymentStatus(paymentID string) (string, map[string]any, error) {
	url := c.buildURL(fmt.Sprintf("/api/v1/payment/%s", paymentID))

	httpReq, err := http.NewRequestWithContext(c.ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeader(httpReq)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", nil, fmt.Errorf("unauthorized: invalid or missing API key")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var payment map[string]any
	if err := decodeJSONResponse(respBody, &payment, resp.Header.Get("Content-Type")); err != nil {
		return "", nil, fmt.Errorf("failed to decode response: %w", err)
	}
	status, _ := payment["status"].(string)

	if status == "" {
		return "", nil, fmt.Errorf("payment status response missing status")
	}

	return strings.ToLower(strings.TrimSpace(status)), payment, nil
}

// executeCommand executes the command using the actuator.
// Returns an optional base64-encoded JPEG image (non-empty only for take_picture on success) and an error.
func (c *Client) executeCommand(cmd *CommandResponse) (string, error) {
	if cmd == nil || cmd.Command == "" {
		return "", nil
	}

	const cancelHoldDuration = 300 * time.Millisecond

	// Acquire lock - blocks if another command is executing
	c.actuatorMutex.Lock()
	defer c.actuatorMutex.Unlock()

	// Determine duration: use API-provided duration_ms if present, otherwise use config default
	var duration time.Duration
	if cmd.DurationMs != nil && *cmd.DurationMs > 0 {
		duration = time.Duration(*cmd.DurationMs) * time.Millisecond
		log.Printf("Device client: executing command %d: %s with API-provided duration %dms", cmd.ID, cmd.Command, *cmd.DurationMs)
	} else {
		duration = time.Duration(c.config.ActuatorMovement) * time.Second
		log.Printf("Device client: executing command %d: %s with default duration %v", cmd.ID, cmd.Command, duration)
	}

	c.setExecutingCommand(cmd)

	switch strings.ToLower(cmd.Command) {
	case "extend":
		err := actuator.Extend(duration)
		if err != nil {
			log.Printf("Device client: failed to execute command %d (%s): %v", cmd.ID, cmd.Command, err)
		}
		return "", err
	case "retract":
		err := actuator.Retract(duration)
		if err != nil {
			log.Printf("Device client: failed to execute command %d (%s): %v", cmd.ID, cmd.Command, err)
		}
		return "", err
	case "home":
		actuator.Home()
		return "", nil
	case "message":
		// Message command: display in UI for specified duration
		log.Printf("Device client: displaying message: %s for %v", cmd.Message, duration)
		// Sleep for the duration to keep the message visible in UI
		time.Sleep(duration)
		return "", nil
	case "cancel":
		log.Printf("Device client: cancel command received, clearing current payment")
		c.SetPaymentID("")
		// Keep the command visible to the UI briefly.
		time.Sleep(cancelHoldDuration)
		return "", nil
	case "restart":
		log.Printf("Device client: restart command received, resetting state machine")
		if err := c.restartStateMachine(); err != nil {
			log.Printf("Device client: restart command failed: %v", err)
			return "", err
		}
		return "", nil
	case "ball_dispenser":
		log.Printf("Device client: ball dispenser cycle requested")
		_, err := c.dispenseAndWaitForBallLocked()
		if err != nil {
			log.Printf("Device client: ball dispenser failed: %v", err)
			return "", err
		}
		paymentID := c.GetPaymentID()
		if paymentID != "" {
			log.Printf("Device client: ball dispenser cycle complete for payment %s", paymentID)
		} else {
			log.Printf("Device client: ball dispenser cycle complete (no active payment)")
		}
		return "", nil
	case "load_test":
		const defaultLoadTestCycles = 15
		loadTestCycles := defaultLoadTestCycles
		if cmd.RepeatCount != nil {
			if *cmd.RepeatCount < 0 {
				return "", fmt.Errorf("load_test command: repeat_count must be >= 0, got %d", *cmd.RepeatCount)
			}
			loadTestCycles = *cmd.RepeatCount
		}

		log.Printf("Device client: load test started (%d simulated successful payments)", loadTestCycles)
		totalBeamCuts := 0
		zeroCutCycles := 0
		totalActuatorMs := 0
		minBeamCuts := -1
		maxBeamCuts := -1
		minActuatorMs := -1
		maxActuatorMs := -1
		for i := 1; i <= loadTestCycles; i++ {
			paymentID := fmt.Sprintf("load-test-payment-%d", i)
			c.setCurrentPayment(paymentID, map[string]any{
				"id":            paymentID,
				"status":        "paid",
				"payment_phase": "waiting_for_payment",
			})
			c.setRuntimeState(StateDispensing, fmt.Sprintf("Load test: simuliere erfolgreiche Zahlung %d/%d", i, loadTestCycles))
			c.updateExecutingCommandMessage(fmt.Sprintf("Payment %d/%d", i, loadTestCycles))
			referenceBaseline := c.sampleBallReferenceBaseline("load_test")

			log.Printf("Device client: load test cycle %d/%d starting dispense", i, loadTestCycles)
			cycleActuatorMs, beamCuts, err := c.triggerWithBreakBeamCount()
			if err != nil {
				log.Printf("Device client: load test failed on cycle %d during dispense: %v", i, err)
				return "", err
			}

			totalActuatorMs += cycleActuatorMs
			totalBeamCuts += beamCuts
			if minBeamCuts == -1 || beamCuts < minBeamCuts {
				minBeamCuts = beamCuts
			}
			if maxBeamCuts == -1 || beamCuts > maxBeamCuts {
				maxBeamCuts = beamCuts
			}
			if minActuatorMs == -1 || cycleActuatorMs < minActuatorMs {
				minActuatorMs = cycleActuatorMs
			}
			if maxActuatorMs == -1 || cycleActuatorMs > maxActuatorMs {
				maxActuatorMs = cycleActuatorMs
			}
			if beamCuts == 0 {
				zeroCutCycles++
				log.Printf("Device client: load test cycle %d/%d dispense complete: total_ms=%d beam_cuts=%d WARNING=no beam interruption detected", i, loadTestCycles, cycleActuatorMs, beamCuts)
			} else {
				log.Printf("Device client: load test cycle %d/%d dispense complete: total_ms=%d beam_cuts=%d", i, loadTestCycles, cycleActuatorMs, beamCuts)
			}

			c.recordDispensedCount(paymentID, beamCuts)

			if err := c.waitForBallReady(true, true, referenceBaseline); err != nil {
				log.Printf("Device client: load test failed on cycle %d after dispense verification: %v (beam_cuts=%d total_ms=%d)", i, err, beamCuts, cycleActuatorMs)
				return "", err
			}

			log.Printf("Device client: load test cycle %d/%d verification complete", i, loadTestCycles)
			c.SetPaymentID("")
			c.setRuntimeState(StateDetectingBall, "Warte auf Ball")
		}

		avgBeamCuts := 0.0
		avgActuatorMs := 0.0
		if loadTestCycles > 0 {
			avgBeamCuts = float64(totalBeamCuts) / float64(loadTestCycles)
			avgActuatorMs = float64(totalActuatorMs) / float64(loadTestCycles)
		}
		log.Printf("Device client: load test complete cycles=%d total_beam_cuts=%d zero_cut_cycles=%d avg_beam_cuts=%.2f min_beam_cuts=%d max_beam_cuts=%d avg_actuator_ms=%.1f min_actuator_ms=%d max_actuator_ms=%d", loadTestCycles, totalBeamCuts, zeroCutCycles, avgBeamCuts, minBeamCuts, maxBeamCuts, avgActuatorMs, minActuatorMs, maxActuatorMs)
		return "", nil
	case "vibrate":
		// Vibrate command: validate percent and duration_ms, then buzz vibrator
		if cmd.Percent == nil {
			return "", fmt.Errorf("vibrate command missing required field: percent")
		}
		if *cmd.Percent < 1 || *cmd.Percent > 100 {
			return "", fmt.Errorf("vibrate command: percent must be between 1 and 100, got %d", *cmd.Percent)
		}
		if cmd.DurationMs == nil {
			return "", fmt.Errorf("vibrate command missing required field: duration_ms")
		}
		if *cmd.DurationMs < 100 || *cmd.DurationMs > 60000 {
			return "", fmt.Errorf("vibrate command: duration_ms must be between 100 and 60000, got %d", *cmd.DurationMs)
		}
		intensity := float64(*cmd.Percent) / 100.0
		dur := time.Duration(*cmd.DurationMs) * time.Millisecond
		log.Printf("Device client: vibrating at %d%% for %dms", *cmd.Percent, *cmd.DurationMs)
		err := vibrator.Buzz(intensity, dur)
		if err != nil {
			log.Printf("Device client: failed to execute command %d (%s): %v", cmd.ID, cmd.Command, err)
		}
		return "", err
	case "take_picture":
		log.Printf("Device client: take_picture command received")
		imgBytes, err := camera.Capture()
		if err != nil {
			log.Printf("Device client: failed to capture image: %v", err)
			return "", err
		}
		imageBase64 := base64.StdEncoding.EncodeToString(imgBytes)
		log.Printf("Device client: image captured (%d bytes)", len(imgBytes))
		return imageBase64, nil
	default:
		return "", fmt.Errorf("unknown command: %s", cmd.Command)
	}
}

// ackCommand acknowledges a command to the server.
// imageData is the base64-encoded JPEG image, required for a successful take_picture ack.
func (c *Client) ackCommand(commandID int, execErr error, imageData string) error {
	url := c.buildURL(fmt.Sprintf("/api/v1/device/commands/%d/ack", commandID))

	// Determine status and error message
	status := "success"
	errorMsg := ""
	if execErr != nil {
		status = "failed"
		errorMsg = execErr.Error()
		// Truncate to 1000 chars max
		if len(errorMsg) > 1000 {
			errorMsg = errorMsg[:1000]
		}
	}

	req := AckRequest{
		Status:       status,
		ErrorMessage: errorMsg,
		ImageBase64:  imageData,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal ack request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(c.ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeader(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized: invalid or missing API key")
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("command not found or belongs to different device")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var ackResp AckResponse
	if err := decodeJSONResponse(respBody, &ackResp, resp.Header.Get("Content-Type")); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !ackResp.Success {
		return fmt.Errorf("server returned success=false")
	}

	log.Printf("Device client: acknowledged command %d", commandID)
	return nil
}

// buildURL constructs the full API URL
func (c *Client) buildURL(path string) string {
	baseURL := strings.TrimRight(c.config.BaendaeliURL, "/")
	path = strings.TrimLeft(path, "/")
	return fmt.Sprintf("%s/%s", baseURL, path)
}

// setAuthHeader adds the authorization header to the request
func (c *Client) setAuthHeader(req *http.Request) {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.config.BaendaeliAPIKey))
}

func decodeJSONResponse(body []byte, target interface{}, contentType string) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("empty response body")
	}

	if err := json.Unmarshal(body, target); err != nil {
		preview := strings.TrimSpace(string(body))
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		if contentType == "" {
			contentType = "unknown"
		}
		return fmt.Errorf("%w (content-type=%q, body=%q)", err, contentType, preview)
	}

	return nil
}

// waitForBallReady calls the colour sensor monitor to confirm a ball is in position.
// When showWaitingMessage is true, it displays a waiting overlay while scanning.
// When allowVibration is false, scanning is passive and never triggers vibrator bursts.
func (c *Client) waitForBallReady(showWaitingMessage bool, allowVibration bool, referenceBaseline *uint16) error {
	if showWaitingMessage {
		c.setRuntimeState(StateDetectingBall, "Warte auf Ball")
		c.setExecutingCommand(&CommandResponse{
			Command: "message",
			Message: "Waiting for Ball Release",
		})
	}

	var observer colorsensor.AttemptObserver
	if showWaitingMessage {
		observer = func(attempt int, maxAttempts int) {
			c.updateExecutingCommandMessage(fmt.Sprintf("Waiting for Ball Release (%d/%d)", attempt, maxAttempts))
		}
	}

	detectionSource, err := c.waitForBallReadyAttempt(allowVibration, referenceBaseline, observer)
	if err != nil {
		if referenceBaseline != nil {
			// Keep a viable reference around for jam recovery scans.
			c.setPendingBallReference(referenceBaseline)
		}
		log.Printf("Device client: ball not detected — showing jam message")
		c.jammed.Store(true)
		c.setRuntimeState(StateBallStuckFunnel, "Ball steckt im Trichter")
		c.setExecutingCommand(&CommandResponse{
			Command: "message",
			Message: "Ball steckt im Trichter. Rufe eine Techniker*in.",
		})
		return err
	}

	if referenceBaseline != nil {
		// Reuse the last known ball-present baseline for the next detecting cycle.
		// This avoids dropping immediately back to movement-only matching.
		c.setPendingBallReference(referenceBaseline)
	}

	c.jammed.Store(false)
	c.setRuntimeState(StateBallOnSensor, "Ball auf Sensor erkannt")
	if detectionSource != "" {
		log.Printf("Device client: ball presence detected by %s", detectionSource)
	}
	c.setExecutingCommand(&CommandResponse{
		Command: "message",
		Message: "Ball auf Sensor erkannt",
	})
	time.Sleep(700 * time.Millisecond)
	c.clearExecutingCommand()
	return nil
}

func (c *Client) waitForBallReadyAttempt(allowVibration bool, referenceBaseline *uint16, observer colorsensor.AttemptObserver) (string, error) {
	if c.detectBreakBeamDuringWindow() {
		if c.config.BreakBeamDebugLogging {
			log.Println("Break-beam: interrupted during detect window, confirming ball presence")
		}
		return "break-beam", nil
	}

	if allowVibration {
		if referenceBaseline != nil {
			return "color-sensor", colorsensor.WaitForBallWithReferenceBaseline(c.colorSensor, vibratorAdapter{}, c.config, log.Default(), observer, *referenceBaseline)
		}
		return "color-sensor", colorsensor.WaitForBall(c.colorSensor, vibratorAdapter{}, c.config, log.Default(), observer)
	}

	if referenceBaseline != nil {
		return "color-sensor", colorsensor.WaitForBallWithReferenceBaseline(c.colorSensor, nil, c.config, log.Default(), observer, *referenceBaseline)
	}
	return "color-sensor", colorsensor.WaitForBall(c.colorSensor, nil, c.config, log.Default(), observer)
}

func (c *Client) detectBreakBeamDuringWindow() bool {
	if c.breakBeamSensor == nil || !c.breakBeamSensor.IsEnabled() {
		return false
	}

	intervalMs := c.config.BreakBeamPollIntervalMs
	if intervalMs <= 0 {
		intervalMs = 10
	}

	const detectWindowMs = 220
	samples := detectWindowMs / intervalMs
	if detectWindowMs%intervalMs != 0 {
		samples++
	}
	if samples < 1 {
		samples = 1
	}

	interval := time.Duration(intervalMs) * time.Millisecond
	if c.config.BreakBeamDebugLogging {
		log.Printf("Break-beam: detect window start (window_ms=%d poll_ms=%d samples=%d)", detectWindowMs, intervalMs, samples)
	}

	for i := 0; i < samples; i++ {
		interrupted, err := c.breakBeamSensor.ReadInterrupted()
		if err != nil {
			if c.config.BreakBeamDebugLogging {
				log.Printf("Break-beam: read error during detect window: %v", err)
			}
		} else if interrupted {
			if c.config.BreakBeamDebugLogging {
				log.Printf("Break-beam: detect window hit at sample %d/%d", i+1, samples)
			}
			return true
		}

		if i+1 < samples {
			time.Sleep(interval)
		}
	}

	if c.config.BreakBeamDebugLogging {
		log.Printf("Break-beam: detect window miss after %d samples", samples)
	}
	return false
}

func (c *Client) isBreakBeamInterrupted() bool {
	if c.breakBeamSensor == nil || !c.breakBeamSensor.IsEnabled() {
		return false
	}
	interrupted, err := c.breakBeamSensor.ReadInterrupted()
	if err != nil {
		if c.config.BreakBeamDebugLogging {
			log.Printf("Break-beam: read error: %v", err)
		}
		return false
	}
	return interrupted
}

func (c *Client) triggerWithBreakBeamCount() (int, int, error) {
	if c.breakBeamSensor == nil || !c.breakBeamSensor.IsEnabled() {
		totalMs, err := actuator.Trigger()
		return totalMs, 1, err
	}

	type triggerResult struct {
		totalMs int
		err     error
	}

	resultCh := make(chan triggerResult, 1)
	go func() {
		totalMs, err := actuator.Trigger()
		resultCh <- triggerResult{totalMs: totalMs, err: err}
	}()

	intervalMs := c.config.BreakBeamPollIntervalMs
	if intervalMs <= 0 {
		intervalMs = 10
	}
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()

	prevInterrupted := c.isBreakBeamInterrupted()
	cuts := 0
	if c.config.BreakBeamDebugLogging {
		log.Printf("Break-beam: monitoring dispense cycle (poll=%dms, initial_interrupted=%t)", intervalMs, prevInterrupted)
	}

	for {
		select {
		case result := <-resultCh:
			if c.config.BreakBeamDebugLogging {
				log.Printf("Break-beam: dispense monitoring complete (cuts=%d)", cuts)
			}
			return result.totalMs, cuts, result.err
		case <-ticker.C:
			interrupted, err := c.breakBeamSensor.ReadInterrupted()
			if err != nil {
				if c.config.BreakBeamDebugLogging {
					log.Printf("Break-beam: read error during dispense monitoring: %v", err)
				}
				continue
			}
			if interrupted && !prevInterrupted {
				cuts++
				if c.config.BreakBeamDebugLogging {
					log.Printf("Break-beam: cut #%d detected during dispense", cuts)
				}
			}
			prevInterrupted = interrupted
		}
	}
}

func (c *Client) runStartupExtractorCycle() error {
	c.setRuntimeState(StateStartupCycle, "Initialzyklus laeuft")
	c.setExecutingCommand(&CommandResponse{
		Command: "message",
		Message: "Starte Initialzyklus",
	})

	// Capture ball-present reference baseline before startup cycle. In the real
	// startup condition a ball is already on the sensor, so this reference is
	// used to detect the same settled-presence state without requiring motion.
	c.captureStartupBallReferenceBaseline()

	if _, err := actuator.Trigger(); err != nil {
		c.clearExecutingCommand()
		return err
	}

	c.clearExecutingCommand()
	return nil
}

func (c *Client) restartStateMachine() error {
	// Clear runtime/session state before running startup steps again.
	c.SetPaymentID("")
	c.clearPendingCommand()
	c.setPendingBallReference(nil)
	c.jammed.Store(false)

	if c.config.ActuatorEnabled {
		c.setRuntimeState(StateStarting, "Homing actuator")
		actuator.Home()

		if err := c.runStartupExtractorCycle(); err != nil {
			c.setRuntimeState(StateError, "Startup cycle failed")
			return err
		}
	}

	c.setRuntimeState(StateDetectingBall, "Warte auf Ball")
	return nil
}

func (c *Client) captureStartupBallReferenceBaseline() {
	if c.colorSensor == nil || !c.colorSensor.IsEnabled() {
		c.setPendingBallReference(nil)
		return
	}

	baseline, err := colorsensor.SampleBaseline(c.colorSensor, log.Default())
	if err != nil {
		log.Printf("Device client: failed to capture startup ball-present reference baseline: %v", err)
		c.setPendingBallReference(nil)
		return
	}

	c.setPendingBallReference(&baseline)
	log.Printf("Device client: captured startup ball-present reference baseline C=%d", baseline)
}

func (c *Client) sampleBallReferenceBaseline(context string) *uint16 {
	if c.colorSensor == nil || !c.colorSensor.IsEnabled() {
		return nil
	}

	baseline, err := colorsensor.SampleBaseline(c.colorSensor, log.Default())
	if err != nil {
		log.Printf("Device client: failed to capture %s reference baseline: %v", context, err)
		return nil
	}

	// Sanity-check: reject readings that fall in the jam/empty band. Storing a jam-level
	// value as a ball-present reference would make the hybrid detector accept empty sensor
	// readings as valid balls on every subsequent cycle.
	if c.config.ColorSensorClearBandEnabled &&
		c.config.ColorSensorClearBallMin > 0 &&
		int(baseline) < c.config.ColorSensorClearBallMin {
		log.Printf("Device client: captured %s reference baseline C=%d rejected — below clear-band ball_min=%d", context, baseline, c.config.ColorSensorClearBallMin)
		return nil
	}

	log.Printf("Device client: captured %s reference baseline C=%d", context, baseline)
	return &baseline
}

// DispenseAndWaitForBall runs one dispense cycle and then waits until the next ball is detected.
func (c *Client) DispenseAndWaitForBall() (int, error) {
	c.actuatorMutex.Lock()
	defer c.actuatorMutex.Unlock()

	return c.dispenseAndWaitForBallLocked()
}

func (c *Client) dispenseAndWaitForBallLocked() (int, error) {

	paymentID := c.GetPaymentID()
	referenceBaseline := c.sampleBallReferenceBaseline("post-dispense")

	totalMs, beamCuts, err := c.triggerWithBreakBeamCount()
	if err != nil {
		return 0, err
	}

	if paymentID != "" {
		c.recordDispensedCount(paymentID, beamCuts)
	}

	if c.config.BreakBeamDebugLogging {
		log.Printf("Device client: dispense beam cut count=%d", beamCuts)
	}

	if err := c.waitForBallReady(true, true, referenceBaseline); err != nil {
		return totalMs, err
	}

	return totalMs, nil
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func paymentPhase(payment map[string]any) string {
	if payment == nil {
		return ""
	}
	phase, _ := payment["payment_phase"].(string)
	return strings.ToLower(strings.TrimSpace(phase))
}

func (c *Client) canExecuteCommandNow(cmd *CommandResponse) bool {
	if cmd == nil {
		return false
	}

	command := strings.ToLower(strings.TrimSpace(cmd.Command))
	if command == "" {
		return false
	}

	// These commands are always safe to execute immediately.
	switch command {
	case "message", "take_picture", "cancel", "restart":
		return true
	}

	// These commands require an idle/clean machine state.
	switch command {
	case "load_test", "ball_dispenser":
		return c.isCleanCommandState()
	case "home", "extend", "retract", "vibrate":
		return c.isActuationCommandState()
	}

	// Unknown commands should still be executed and fail through normal validation/ack flow.
	return true
}

func (c *Client) isCleanCommandState() bool {
	if c.jammed.Load() {
		return false
	}

	if c.GetPaymentID() != "" {
		return false
	}

	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()

	return c.state == StateDetectingBall || c.state == StateIdle
}

func (c *Client) isActuationCommandState() bool {
	if c.jammed.Load() {
		return false
	}

	paymentID := c.GetPaymentID()
	if paymentID == "" {
		return c.isCleanCommandState()
	}

	// While waiting for user payment confirmation, only non-actuation commands are allowed.
	if c.currentPaymentPhase() == "waiting_for_payment" {
		return false
	}

	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()

	switch c.state {
	case StateStarting, StateStartupCycle, StateDispensing, StateCommandExecuting, StateError, StateJam:
		return false
	default:
		return true
	}
}

func (c *Client) currentPaymentPhase() string {
	return paymentPhase(c.getCurrentPayment())
}

type vibratorAdapter struct{}

func (vibratorAdapter) Buzz(intensity float64, duration time.Duration) error {
	return vibrator.Buzz(intensity, duration)
}
