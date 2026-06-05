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
	ID         int    `json:"id"`
	Command    string `json:"command"`
	DurationMs *int   `json:"duration_ms,omitempty"` // Optional duration in milliseconds
	Message    string `json:"message,omitempty"`     // Message text for message command
	Percent    *int   `json:"percent,omitempty"`     // Vibration intensity (1-100) for vibrate command
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

	// Actuator lock to prevent concurrent commands
	actuatorMutex sync.Mutex
}

// New creates a new device client
func New(cfg *config.Config) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		config:       cfg,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		ctx:          ctx,
		cancel:       cancel,
		pollInterval: 7 * time.Second,
		colorSensor:  colorsensor.New(cfg),
		state:        StateStarting,
	}
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

	if c.config.ActuatorEnabled {
		if err := c.runStartupExtractorCycle(); err != nil {
			c.setRuntimeState(StateError, "Startup cycle failed")
			log.Printf("Device client: startup extractor cycle failed: %v", err)
		}
	}

	c.setRuntimeState(StateDetectingBall, "Warte auf Ball")

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
	if err := c.colorSensor.Close(); err != nil {
		log.Printf("Device client: failed to close colour sensor: %v", err)
	}
	log.Println("Device client stopped")
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
		if err := c.waitForBallReady(false, false, nil); err != nil {
			return
		}
		// Jam cleared, continue normal polling and command handling.
		c.setRuntimeState(StateDetectingBall, "Stau behoben")
		log.Printf("Device client: jam cleared by passive ball detection")
	}

	// If a jam is still active after passive scan attempt, skip command execution.
	if c.jammed.Load() {
		c.setRuntimeState(StateJam, "Stau detektiert")
		return
	}

	c.runStateMachineCycle()

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

		if !c.jammed.Load() {
			c.clearExecutingCommand()
			if c.GetPaymentID() == "" {
				c.setRuntimeState(StateDetectingBall, "Warte auf Ball")
			}
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
		if _, err := c.DispenseAndWaitForBall(); err != nil {
			c.setRuntimeState(StateError, "Ausgabe fehlgeschlagen")
			log.Printf("Device client: dispense failed after successful payment: %v", err)
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

	c.clearPendingDispense(paymentID, dispensedCount)

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
		const defaultLoadTestCycles = 30
		loadTestCycles := defaultLoadTestCycles
		if cmd.DurationMs != nil && *cmd.DurationMs > 0 {
			loadTestCycles = *cmd.DurationMs
		}

		log.Printf("Device client: load test started (%d simulated successful payments)", loadTestCycles)
		for i := 1; i <= loadTestCycles; i++ {
			paymentID := fmt.Sprintf("load-test-payment-%d", i)
			c.setCurrentPayment(paymentID, map[string]any{
				"id":            paymentID,
				"status":        "paid",
				"payment_phase": "waiting_for_payment",
			})
			c.setRuntimeState(StateDispensing, fmt.Sprintf("Load test: simuliere erfolgreiche Zahlung %d/%d", i, loadTestCycles))
			c.updateExecutingCommandMessage(fmt.Sprintf("Payment %d/%d", i, loadTestCycles))
			if _, err := actuator.Trigger(); err != nil {
				log.Printf("Device client: load test failed on cycle %d during dispense: %v", i, err)
				return "", err
			}
			// For load tests, avoid reference-baseline matching to prevent false positives
			// from stale geometry shifts between cycles.
			if err := c.waitForBallReady(true, true, nil); err != nil {
				log.Printf("Device client: load test failed on cycle %d: %v", i, err)
				return "", err
			}
			c.SetPaymentID("")
			c.setRuntimeState(StateDetectingBall, "Warte auf Ball")
		}
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

	var err error
	var observer colorsensor.AttemptObserver
	if showWaitingMessage {
		observer = func(attempt int, maxAttempts int) {
			c.updateExecutingCommandMessage(fmt.Sprintf("Waiting for Ball Release (%d/%d)", attempt, maxAttempts))
		}
	}

	if allowVibration {
		if referenceBaseline != nil {
			err = colorsensor.WaitForBallWithReferenceBaseline(c.colorSensor, vibratorAdapter{}, c.config, log.Default(), observer, *referenceBaseline)
		} else {
			err = colorsensor.WaitForBall(c.colorSensor, vibratorAdapter{}, c.config, log.Default(), observer)
		}
	} else {
		if referenceBaseline != nil {
			err = colorsensor.WaitForBallWithReferenceBaseline(c.colorSensor, nil, c.config, log.Default(), observer, *referenceBaseline)
		} else {
			err = colorsensor.WaitForBall(c.colorSensor, nil, c.config, log.Default(), observer)
		}
	}
	if err != nil {
		log.Printf("Device client: ball not detected — showing jam message")
		c.jammed.Store(true)
		c.setRuntimeState(StateBallStuckFunnel, "Ball steckt im Trichter")
		c.setExecutingCommand(&CommandResponse{
			Command: "message",
			Message: "Ball steckt im Trichter. Rufe eine Techniker*in.",
		})
		return err
	}

	c.jammed.Store(false)
	c.setRuntimeState(StateBallOnSensor, "Ball auf Sensor erkannt")
	c.setExecutingCommand(&CommandResponse{
		Command: "message",
		Message: "Ball auf Sensor erkannt",
	})
	time.Sleep(700 * time.Millisecond)
	c.clearExecutingCommand()
	return nil
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

// DispenseAndWaitForBall runs one dispense cycle and then waits until the next ball is detected.
func (c *Client) DispenseAndWaitForBall() (int, error) {
	c.actuatorMutex.Lock()
	defer c.actuatorMutex.Unlock()

	return c.dispenseAndWaitForBallLocked()
}

func (c *Client) dispenseAndWaitForBallLocked() (int, error) {

	paymentID := c.GetPaymentID()
	var preDispenseBaseline *uint16
	if c.colorSensor != nil && c.colorSensor.IsEnabled() {
		if baseline, baselineErr := colorsensor.SampleBaseline(c.colorSensor, log.Default()); baselineErr == nil {
			preDispenseBaseline = &baseline
			log.Printf("Device client: captured pre-dispense baseline C=%d", baseline)
		} else {
			log.Printf("Device client: failed to capture pre-dispense baseline: %v", baselineErr)
		}
	}

	totalMs, err := actuator.Trigger()
	if err != nil {
		return 0, err
	}

	if paymentID != "" {
		c.recordDispensedCount(paymentID, 1)
	}

	if err := c.waitForBallReady(true, true, preDispenseBaseline); err != nil {
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
	case "message", "take_picture", "cancel":
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
