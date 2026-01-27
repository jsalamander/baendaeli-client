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
)

// StatusRequest is sent to the server
type StatusRequest struct {
	PaymentID string `json:"payment_id"`
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
	Message    string `json:"message,omitempty"`      // Message text for message command
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

// Client polls the device API and executes commands
type Client struct {
	config          *config.Config
	httpClient      *http.Client
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	pollInterval    time.Duration
	paymentIDMutex  sync.Mutex
	currentPaymentID string
	running         atomic.Bool
	
	// Command execution status
	statusMutex      sync.Mutex
	executingCommand *CommandResponse
	lastCommandError  string  // Error from last command execution
	
	// Actuator lock to prevent concurrent commands
	actuatorMutex   sync.Mutex
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
	}
}

// SetPaymentID updates the current payment ID
func (c *Client) SetPaymentID(paymentID string) {
	c.paymentIDMutex.Lock()
	defer c.paymentIDMutex.Unlock()
	c.currentPaymentID = paymentID
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
	req := StatusRequest{PaymentID: paymentID}

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

	var statusResp StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !statusResp.Success {
		return fmt.Errorf("server returned success=false")
	}

	return nil
}

// getCommand fetches the next pending command from the server
func (c *Client) getCommand() (*CommandResponse, error) {
	url := c.buildURL("/api/v1/device/commands")

	httpReq, err := http.NewRequestWithContext(c.ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeader(httpReq)

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

	var cmdResp CommandResponse
	if err := json.NewDecoder(resp.Body).Decode(&cmdResp); err != nil {
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

	var ackResp AckResponse
	if err := json.NewDecoder(resp.Body).Decode(&ackResp); err != nil {
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
