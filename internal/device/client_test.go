package device

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jsalamander/baendaeli-client/internal/config"
)

func TestReportStatus(t *testing.T) {
	tests := []struct {
		name        string
		paymentID   string
		serverCode  int
		serverResp  string
		expectError bool
	}{
		{
			name:       "successful status report",
			paymentID:  "test-payment-123",
			serverCode: http.StatusOK,
			serverResp: `{"success": true}`,
		},
		{
			name:        "unauthorized",
			paymentID:   "test-payment-123",
			serverCode:  http.StatusUnauthorized,
			serverResp:  `{"error": "unauthorized"}`,
			expectError: true,
		},
		{
			name:        "server error",
			paymentID:   "test-payment-123",
			serverCode:  http.StatusInternalServerError,
			serverResp:  `{"error": "server error"}`,
			expectError: true,
		},
		{
			name:        "success false",
			paymentID:   "test-payment-123",
			serverCode:  http.StatusOK,
			serverResp:  `{"success": false}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request
				if !strings.Contains(r.URL.Path, "/api/v1/device/status") {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				// Check auth header
				authHeader := r.Header.Get("Authorization")
				if authHeader == "" {
					t.Error("missing authorization header")
				}

				// Check method
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}

				// Send response
				w.WriteHeader(tt.serverCode)
				w.Write([]byte(tt.serverResp))
			}))
			defer server.Close()

			// Create client with mock server URL
			cfg := &config.Config{
				BaendaeliURL:    server.URL,
				BaendaeliAPIKey: "test-key",
			}
			client := New(cfg)

			err := client.reportStatus(tt.paymentID)
			if (err != nil) != tt.expectError {
				t.Errorf("expected error=%v, got error=%v", tt.expectError, err)
			}
		})
	}
}

func TestGetCommand(t *testing.T) {
	tests := []struct {
		name         string
		serverCode   int
		serverResp   string
		expectError  bool
		expectedCmd  *CommandResponse
	}{
		{
			name:       "command available",
			serverCode: http.StatusOK,
			serverResp: `{"id": 42, "command": "extend"}`,
			expectedCmd: &CommandResponse{
				ID:      42,
				Command: "extend",
			},
		},
		{
			name:       "no command",
			serverCode: http.StatusOK,
			serverResp: `{"command": null}`,
		},
		{
			name:        "unauthorized",
			serverCode:  http.StatusUnauthorized,
			serverResp:  `{"error": "unauthorized"}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Path, "/api/v1/device/commands") {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}

				w.WriteHeader(tt.serverCode)
				w.Write([]byte(tt.serverResp))
			}))
			defer server.Close()

			cfg := &config.Config{
				BaendaeliURL:    server.URL,
				BaendaeliAPIKey: "test-key",
			}
			client := New(cfg)

			cmd, err := client.getCommand()
			if (err != nil) != tt.expectError {
				t.Errorf("expected error=%v, got error=%v", tt.expectError, err)
			}

			if tt.expectedCmd != nil {
				if cmd == nil {
					t.Error("expected command, got nil")
				} else if cmd.ID != tt.expectedCmd.ID || cmd.Command != tt.expectedCmd.Command {
					t.Errorf("expected %+v, got %+v", tt.expectedCmd, cmd)
				}
			} else if cmd != nil {
				t.Errorf("expected nil command, got %+v", cmd)
			}
		})
	}
}

func TestAckCommand(t *testing.T) {
	tests := []struct {
		name        string
		commandID   int
		execErr     error
		serverCode  int
		serverResp  string
		expectError bool
	}{
		{
			name:       "successful ack",
			commandID:  42,
			execErr:    nil,
			serverCode: http.StatusOK,
			serverResp: `{"success": true}`,
		},
		{
			name:        "ack with command error",
			commandID:   42,
			execErr:     fmt.Errorf("motor failed"),
			serverCode:  http.StatusOK,
			serverResp:  `{"success": true}`,
			expectError: false,
		},
		{
			name:        "command not found",
			commandID:   999,
			execErr:     nil,
			serverCode:  http.StatusNotFound,
			serverResp:  `{"error": "not found"}`,
			expectError: true,
		},
		{
			name:        "unauthorized",
			commandID:   42,
			execErr:     nil,
			serverCode:  http.StatusUnauthorized,
			serverResp:  `{"error": "unauthorized"}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/api/v1/device/commands/42/ack"
				if !strings.Contains(r.URL.Path, expectedPath) {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}

				// Verify request body contains status and error_message
				var ackReq AckRequest
				if err := json.NewDecoder(r.Body).Decode(&ackReq); err == nil {
					if tt.execErr == nil && ackReq.Status != "success" {
						t.Errorf("expected status=success, got %s", ackReq.Status)
					}
					if tt.execErr != nil && ackReq.Status != "failed" {
						t.Errorf("expected status=failed, got %s", ackReq.Status)
					}
				}

				w.WriteHeader(tt.serverCode)
				w.Write([]byte(tt.serverResp))
			}))
			defer server.Close()

			cfg := &config.Config{
				BaendaeliURL:    server.URL,
				BaendaeliAPIKey: "test-key",
			}
			client := New(cfg)

			err := client.ackCommand(42, tt.execErr)
			if (err != nil) != tt.expectError {
				t.Errorf("expected error=%v, got error=%v", tt.expectError, err)
			}
		})
	}
}

func TestSetPaymentID(t *testing.T) {
	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	paymentID := "payment-uuid-123"
	client.SetPaymentID(paymentID)

	retrieved := client.GetPaymentID()
	if retrieved != paymentID {
		t.Errorf("expected %s, got %s", paymentID, retrieved)
	}
}

func TestBuildURL(t *testing.T) {
	tests := []struct {
		baseURL  string
		path     string
		expected string
	}{
		{
			baseURL:  "http://example.com",
			path:     "/api/v1/device/status",
			expected: "http://example.com/api/v1/device/status",
		},
		{
			baseURL:  "http://example.com/",
			path:     "api/v1/device/commands",
			expected: "http://example.com/api/v1/device/commands",
		},
		{
			baseURL:  "https://api.example.com/v1/",
			path:     "/api/v1/device/status",
			expected: "https://api.example.com/v1/api/v1/device/status",
		},
	}

	for _, tt := range tests {
		cfg := &config.Config{
			BaendaeliURL:    tt.baseURL,
			BaendaeliAPIKey: "test-key",
		}
		client := New(cfg)

		result := client.buildURL(tt.path)
		if result != tt.expected {
			t.Errorf("for %s + %s: expected %s, got %s", tt.baseURL, tt.path, tt.expected, result)
		}
	}
}

func TestStartStop(t *testing.T) {
	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	// Start client
	client.Start()
	time.Sleep(100 * time.Millisecond) // Give goroutine time to start

	if !client.running.Load() {
		t.Error("expected client to be running")
	}

	// Stop client
	client.Stop()
	time.Sleep(100 * time.Millisecond) // Give goroutine time to stop

	if client.running.Load() {
		t.Error("expected client to be stopped")
	}
}

func TestPoll(t *testing.T) {
	// Create a mock server that tracks calls
	statusCalled := false
	commandCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/status") {
			statusCalled = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success": true}`))
		} else if strings.Contains(r.URL.Path, "/commands") {
			commandCalled = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"command": null}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaendaeliURL:    server.URL,
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	client.poll()

	if !statusCalled {
		t.Error("expected status endpoint to be called")
	}
	if !commandCalled {
		t.Error("expected commands endpoint to be called")
	}
}

func TestMarshalStatusRequest(t *testing.T) {
	req := StatusRequest{
		PaymentID: "test-payment-id",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	expected := `{"payment_id":"test-payment-id"}`
	if strings.TrimSpace(string(data)) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}

func TestExecutingCommandTracking(t *testing.T) {
	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	// Initially should be nil
	if client.GetExecutingCommand() != nil {
		t.Error("expected no executing command initially")
	}

	// Set a command
	cmd := &CommandResponse{
		ID:      42,
		Command: "extend",
	}
	client.setExecutingCommand(cmd)

	// Should be able to retrieve it
	retrieved := client.GetExecutingCommand()
	if retrieved == nil {
		t.Error("expected executing command to be set")
	} else if retrieved.ID != 42 || retrieved.Command != "extend" {
		t.Errorf("expected %+v, got %+v", cmd, retrieved)
	}

	// Clear it
	client.clearExecutingCommand()
	if client.GetExecutingCommand() != nil {
		t.Error("expected executing command to be cleared")
	}
}

func TestActuatorLockPreventsRaceConditions(t *testing.T) {
	cfg := &config.Config{
		BaendaeliURL:           "http://example.com",
		BaendaeliAPIKey:        "test-key",
		ActuatorMovement:       1,
		ActuatorEnabled:        false,
	}
	client := New(cfg)

	// Test that lock can be acquired and released
	client.actuatorMutex.Lock()
	locked := true
	client.actuatorMutex.Unlock()
	locked = false

	if locked {
		t.Error("expected mutex to be unlocked")
	}

	// Test that two goroutines try to acquire lock sequentially
	var lockOrder []int
	var orderMutex sync.Mutex

	go func() {
		client.actuatorMutex.Lock()
		defer client.actuatorMutex.Unlock()
		orderMutex.Lock()
		lockOrder = append(lockOrder, 1)
		orderMutex.Unlock()
		time.Sleep(50 * time.Millisecond)
	}()

	time.Sleep(10 * time.Millisecond) // Ensure first goroutine gets lock first

	go func() {
		client.actuatorMutex.Lock()
		defer client.actuatorMutex.Unlock()
		orderMutex.Lock()
		lockOrder = append(lockOrder, 2)
		orderMutex.Unlock()
	}()

	time.Sleep(150 * time.Millisecond) // Wait for both to complete

	// Verify order: first goroutine should get lock first
	if len(lockOrder) != 2 {
		t.Errorf("expected 2 lock acquisitions, got %d", len(lockOrder))
	} else if lockOrder[0] != 1 || lockOrder[1] != 2 {
		t.Errorf("expected lock order [1, 2], got %v", lockOrder)
	}
}

func TestLockActuatorPublicMethods(t *testing.T) {
	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	// Test public lock/unlock methods
	client.LockActuator()
	
	// Verify lock was acquired by checking it blocks another acquire
	acquired := make(chan bool, 1)
	go func() {
		// Try to acquire - should block until we unlock
		client.LockActuator()
		acquired <- true
		client.UnlockActuator()
	}()

	// Give goroutine time to block
	time.Sleep(50 * time.Millisecond)

	// At this point goroutine should be blocked, channel should be empty
	select {
	case <-acquired:
		t.Error("expected lock to be held, but goroutine was able to acquire it")
	default:
		// Good - goroutine is blocked
	}

	// Now unlock
	client.UnlockActuator()

	// Give goroutine time to acquire and release
	time.Sleep(50 * time.Millisecond)

	// Now it should have completed
	select {
	case <-acquired:
		// Good - goroutine acquired and released
	default:
		t.Error("expected lock to be released, but goroutine did not acquire it")
	}
}

func TestCommandWithDurationMs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/commands") && r.Method == http.MethodGet {
			// Return command with duration_ms
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id": 43, "command": "extend", "duration_ms": 30000}`))
		} else if strings.Contains(r.URL.Path, "/status") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success": true}`))
		} else if strings.Contains(r.URL.Path, "/ack") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success": true}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaendaeliURL:     server.URL,
		BaendaeliAPIKey:  "test-key",
		ActuatorMovement: 2, // Default 2 seconds
	}
	client := New(cfg)

	cmd, err := client.getCommand()
	if err != nil {
		t.Fatalf("failed to get command: %v", err)
	}

	if cmd == nil {
		t.Fatal("expected command, got nil")
	}

	if cmd.ID != 43 {
		t.Errorf("expected ID 43, got %d", cmd.ID)
	}

	if cmd.Command != "extend" {
		t.Errorf("expected command 'extend', got %s", cmd.Command)
	}

	if cmd.DurationMs == nil {
		t.Fatal("expected duration_ms to be set")
	}

	if *cmd.DurationMs != 30000 {
		t.Errorf("expected duration_ms 30000, got %d", *cmd.DurationMs)
	}
}

func TestCommandWithoutDurationMs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/commands") && r.Method == http.MethodGet {
			// Return command without duration_ms
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id": 44, "command": "retract"}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaendaeliURL:     server.URL,
		BaendaeliAPIKey:  "test-key",
		ActuatorMovement: 2,
	}
	client := New(cfg)

	cmd, err := client.getCommand()
	if err != nil {
		t.Fatalf("failed to get command: %v", err)
	}

	if cmd == nil {
		t.Fatal("expected command, got nil")
	}

	if cmd.DurationMs != nil {
		t.Errorf("expected duration_ms to be nil, got %d", *cmd.DurationMs)
	}
}

func TestCommandWithZeroDurationMs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/commands") && r.Method == http.MethodGet {
			// Return command with duration_ms set to 0
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id": 45, "command": "extend", "duration_ms": 0}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaendaeliURL:     server.URL,
		BaendaeliAPIKey:  "test-key",
		ActuatorMovement: 2,
	}
	client := New(cfg)

	cmd, err := client.getCommand()
	if err != nil {
		t.Fatalf("failed to get command: %v", err)
	}

	if cmd == nil {
		t.Fatal("expected command, got nil")
	}

	if cmd.DurationMs == nil {
		t.Fatal("expected duration_ms to be set")
	}

	if *cmd.DurationMs != 0 {
		t.Errorf("expected duration_ms 0, got %d", *cmd.DurationMs)
	}

	// When executing, duration of 0 should fall back to default (tested in executeCommand logic)
}

func TestCommandMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/commands") && r.Method == http.MethodGet {
			// Return message command
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id": 46, "command": "message", "message": "Hello Device!", "duration_ms": 100}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaendaeliURL:     server.URL,
		BaendaeliAPIKey:  "test-key",
		ActuatorMovement: 2,
	}
	client := New(cfg)

	cmd, err := client.getCommand()
	if err != nil {
		t.Fatalf("failed to get command: %v", err)
	}

	if cmd == nil {
		t.Fatal("expected command, got nil")
	}

	if cmd.ID != 46 {
		t.Errorf("expected ID 46, got %d", cmd.ID)
	}

	if cmd.Command != "message" {
		t.Errorf("expected command 'message', got %s", cmd.Command)
	}

	if cmd.Message != "Hello Device!" {
		t.Errorf("expected message 'Hello Device!', got %s", cmd.Message)
	}

	if cmd.DurationMs == nil {
		t.Fatal("expected duration_ms to be set")
	}

	if *cmd.DurationMs != 100 {
		t.Errorf("expected duration_ms 100, got %d", *cmd.DurationMs)
	}

	// Test that executing the message command works and takes the right duration
	start := time.Now()
	err = client.executeCommand(cmd)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("expected no error executing message command, got %v", err)
	}

	// Should have slept for ~100ms
	if elapsed < 90*time.Millisecond || elapsed > 200*time.Millisecond {
		t.Errorf("expected message command to take ~100ms, took %v", elapsed)
	}
}

func TestCommandBallDispenser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/commands") && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id": 47, "command": "ball_dispenser"}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaendaeliURL:    server.URL,
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	cmd, err := client.getCommand()
	if err != nil {
		t.Fatalf("failed to get command: %v", err)
	}

	if cmd == nil {
		t.Fatal("expected command, got nil")
	}

	if cmd.ID != 47 {
		t.Errorf("expected ID 47, got %d", cmd.ID)
	}

	if cmd.Command != "ball_dispenser" {
		t.Errorf("expected command 'ball_dispenser', got %s", cmd.Command)
	}
}

func TestCommandCancelClearsPayment(t *testing.T) {
	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	client.SetPaymentID("payment-uuid-123")

	cmd := &CommandResponse{
		ID:      50,
		Command: "cancel",
	}

	start := time.Now()
	err := client.executeCommand(cmd)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("expected no error executing cancel command, got %v", err)
	}

	if client.GetPaymentID() != "" {
		t.Errorf("expected payment id to be cleared, got %s", client.GetPaymentID())
	}

	if elapsed < 200*time.Millisecond {
		t.Errorf("expected cancel command to take at least 200ms, took %v", elapsed)
	}
}
