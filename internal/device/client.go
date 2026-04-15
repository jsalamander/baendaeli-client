package device

import (
	"bytes"
	"context"
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
	"github.com/jsalamander/baendaeli-client/internal/config"
	"github.com/jsalamander/baendaeli-client/internal/irsensor"
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
	Status       string `json:"status"`        // "success" or "failed"
	ErrorMessage string `json:"error_message"` // max 1000 chars, only for failed status
}

// AckResponse is received from the server
type AckResponse struct {
	Success bool `json:"success"`
}

type dispenseMonitor interface {
	Measure(func() error) (int, error)
	Close() error
}

type pendingDispense struct {
	paymentID string
	count     int
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
	running          atomic.Bool
	dispenseMonitor  dispenseMonitor

	// Command execution status
	statusMutex      sync.Mutex
	executingCommand *CommandResponse
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
		config:          cfg,
		httpClient:      &http.Client{Timeout: 15 * time.Second},
		ctx:             ctx,
		cancel:          cancel,
		pollInterval:    7 * time.Second,
		dispenseMonitor: irsensor.New(cfg),
	}
}

// SetPaymentID updates the current payment ID
func (c *Client) SetPaymentID(paymentID string) {
	c.paymentIDMutex.Lock()
	defer c.paymentIDMutex.Unlock()
	c.currentPaymentID = paymentID
	c.dropStalePendingDispense(paymentID)
}

// GetPaymentID returns the current payment ID
func (c *Client) GetPaymentID() string {
	c.paymentIDMutex.Lock()
	defer c.paymentIDMutex.Unlock()
	return c.currentPaymentID
}

// GetExecutingCommand returns the currently executing command, if any
func (c *Client) GetExecutingCommand() *CommandResponse {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	return c.executingCommand
}

// setExecutingCommand sets the currently executing command
func (c *Client) setExecutingCommand(cmd *CommandResponse) {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	c.executingCommand = cmd
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
	if err := c.dispenseMonitor.Close(); err != nil {
		log.Printf("Device client: failed to close IR sensor monitor: %v", err)
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

	// 2. Get next command
	cmd, err := c.getCommand()
	if err != nil {
		log.Printf("Device client: failed to get command: %v", err)
		return
	}

	// 3. If command is not null, execute it
	if cmd != nil && cmd.Command != "" {
		execErr := c.executeCommand(cmd)
		if execErr != nil {
			log.Printf("Device client: failed to execute command %d (%s): %v", cmd.ID, cmd.Command, execErr)
		}

		// 4. Acknowledge the command with success/failure status
		if err := c.ackCommand(cmd.ID, execErr); err != nil {
			log.Printf("Device client: failed to acknowledge command %d: %v", cmd.ID, err)
		}

		c.clearExecutingCommand()
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

// executeCommand executes the command using the actuator
func (c *Client) executeCommand(cmd *CommandResponse) error {
	if cmd == nil || cmd.Command == "" {
		return nil
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
		return err
	case "retract":
		err := actuator.Retract(duration)
		if err != nil {
			log.Printf("Device client: failed to execute command %d (%s): %v", cmd.ID, cmd.Command, err)
		}
		return err
	case "home":
		actuator.Home()
		return nil
	case "message":
		// Message command: display in UI for specified duration
		log.Printf("Device client: displaying message: %s for %v", cmd.Message, duration)
		// Sleep for the duration to keep the message visible in UI
		time.Sleep(duration)
		return nil
	case "cancel":
		log.Printf("Device client: cancel command received, clearing current payment")
		c.SetPaymentID("")
		// Keep the command visible to the UI briefly.
		time.Sleep(cancelHoldDuration)
		return nil
	case "ball_dispenser":
		log.Printf("Device client: ball dispenser cycle requested")
		paymentID := c.GetPaymentID()
		dispensedCount, err := c.dispenseMonitor.Measure(func() error {
			_, triggerErr := actuator.Trigger()
			return triggerErr
		})
		c.recordDispensedCount(paymentID, dispensedCount)
		if paymentID != "" {
			log.Printf("Device client: ball dispenser detected %d dispense event(s) for payment %s", dispensedCount, paymentID)
		} else {
			log.Printf("Device client: ball dispenser detected %d dispense event(s) without an active payment", dispensedCount)
		}
		if err != nil {
			log.Printf("Device client: ball dispenser failed: %v", err)
		}
		return err
	case "load_test":
		log.Printf("Device client: load test started (30 cycles)")
		const loadTestCycles = 30
		for i := 1; i <= loadTestCycles; i++ {
			c.updateExecutingCommandMessage(fmt.Sprintf("%d/%d", i, loadTestCycles))
			if _, err := actuator.Trigger(); err != nil {
				log.Printf("Device client: load test failed on cycle %d: %v", i, err)
				return err
			}
		}
		return nil
	case "vibrate":
		// Vibrate command: validate percent and duration_ms, then buzz vibrator
		if cmd.Percent == nil {
			return fmt.Errorf("vibrate command missing required field: percent")
		}
		if *cmd.Percent < 1 || *cmd.Percent > 100 {
			return fmt.Errorf("vibrate command: percent must be between 1 and 100, got %d", *cmd.Percent)
		}
		if cmd.DurationMs == nil {
			return fmt.Errorf("vibrate command missing required field: duration_ms")
		}
		if *cmd.DurationMs < 100 || *cmd.DurationMs > 60000 {
			return fmt.Errorf("vibrate command: duration_ms must be between 100 and 60000, got %d", *cmd.DurationMs)
		}
		intensity := float64(*cmd.Percent) / 100.0
		dur := time.Duration(*cmd.DurationMs) * time.Millisecond
		log.Printf("Device client: vibrating at %d%% for %dms", *cmd.Percent, *cmd.DurationMs)
		err := vibrator.Buzz(intensity, dur)
		if err != nil {
			log.Printf("Device client: failed to execute command %d (%s): %v", cmd.ID, cmd.Command, err)
		}
		return err
	default:
		return fmt.Errorf("unknown command: %s", cmd.Command)
	}
}

// ackCommand acknowledges a command to the server
func (c *Client) ackCommand(commandID int, execErr error) error {
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
