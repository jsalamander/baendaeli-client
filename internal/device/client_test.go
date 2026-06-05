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

	"github.com/jsalamander/baendaeli-client/internal/colorsensor"
	"github.com/jsalamander/baendaeli-client/internal/config"
)

func TestReportStatus(t *testing.T) {
	tests := []struct {
		name        string
		paymentID   string
		serverCode  int
		serverResp  string
		contentType string
		expectError bool
		errorHas    string
	}{
		{
			name:        "successful status report",
			paymentID:   "test-payment-123",
			serverCode:  http.StatusOK,
			serverResp:  `{"success": true}`,
			contentType: "application/json",
		},
		{
			name:        "unauthorized",
			paymentID:   "test-payment-123",
			serverCode:  http.StatusUnauthorized,
			serverResp:  `{"error": "unauthorized"}`,
			contentType: "application/json",
			expectError: true,
		},
		{
			name:        "server error",
			paymentID:   "test-payment-123",
			serverCode:  http.StatusInternalServerError,
			serverResp:  `{"error": "server error"}`,
			contentType: "application/json",
			expectError: true,
		},
		{
			name:        "success false",
			paymentID:   "test-payment-123",
			serverCode:  http.StatusOK,
			serverResp:  `{"success": false}`,
			contentType: "application/json",
			expectError: true,
		},
		{
			name:        "html body with 200 returns descriptive decode error",
			paymentID:   "test-payment-123",
			serverCode:  http.StatusOK,
			serverResp:  `<html><body>gateway error</body></html>`,
			contentType: "text/html",
			expectError: true,
			errorHas:    "content-type=\"text/html\"",
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
				if tt.contentType != "" {
					w.Header().Set("Content-Type", tt.contentType)
				}
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
			if tt.errorHas != "" && (err == nil || !strings.Contains(err.Error(), tt.errorHas)) {
				t.Errorf("expected error to contain %q, got %v", tt.errorHas, err)
			}
		})
	}
}

func TestReportStatusIncludesDispensedCountAndClearsIt(t *testing.T) {
	var received StatusRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success": true}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		BaendaeliURL:    server.URL,
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)
	client.recordDispensedCount("payment-123", 4)

	if err := client.reportStatus("payment-123"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if received.DispensedCount == nil {
		t.Fatal("expected dispensed_count in request")
	}
	if *received.DispensedCount != 4 {
		t.Fatalf("expected dispensed_count=4, got %d", *received.DispensedCount)
	}
	if client.pendingDispensedCount("payment-123") != nil {
		t.Fatal("expected pending dispensed count to be cleared after successful report")
	}
}

func TestReportStatusIncludesZeroDispensedCountWhenNoPending(t *testing.T) {
	var received StatusRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success": true}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		BaendaeliURL:    server.URL,
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	if err := client.reportStatus("payment-123"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if received.DispensedCount == nil {
		t.Fatal("expected dispensed_count in request")
	}
	if *received.DispensedCount != 0 {
		t.Fatalf("expected dispensed_count=0, got %d", *received.DispensedCount)
	}
}

func TestReportStatusKeepsDispensedCountOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "server error"}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		BaendaeliURL:    server.URL,
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)
	client.recordDispensedCount("payment-123", 2)

	if err := client.reportStatus("payment-123"); err == nil {
		t.Fatal("expected reportStatus to fail")
	}

	pending := client.pendingDispensedCount("payment-123")
	if pending == nil || *pending != 2 {
		t.Fatalf("expected pending count to remain at 2, got %v", pending)
	}
}

func TestGetCommand(t *testing.T) {
	tests := []struct {
		name        string
		serverCode  int
		serverResp  string
		expectError bool
		expectedCmd *CommandResponse
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

			err := client.ackCommand(42, tt.execErr, "")
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
	paymentID := "test-payment-id"
	req := StatusRequest{
		PaymentID:     &paymentID,
		ClientVersion: "dev",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	expected := `{"payment_id":"test-payment-id","client_version":"dev"}`
	if strings.TrimSpace(string(data)) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}

func TestMarshalStatusRequestWithDispensedCount(t *testing.T) {
	count := 7
	paymentID := "test-payment-id"
	req := StatusRequest{
		PaymentID:      &paymentID,
		ClientVersion:  "dev",
		DispensedCount: &count,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	expected := `{"payment_id":"test-payment-id","client_version":"dev","dispensed_count":7}`
	if strings.TrimSpace(string(data)) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}

func TestMarshalStatusRequestWithoutPaymentID(t *testing.T) {
	req := StatusRequest{
		ClientVersion: "dev",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	expected := `{"client_version":"dev"}`
	if strings.TrimSpace(string(data)) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}

func TestRecordDispensedCountAccumulatesPerPayment(t *testing.T) {
	client := New(&config.Config{})

	client.recordDispensedCount("payment-123", 1)
	client.recordDispensedCount("payment-123", 2)

	pending := client.pendingDispensedCount("payment-123")
	if pending == nil || *pending != 3 {
		t.Fatalf("expected accumulated count of 3, got %v", pending)
	}
}

func TestSetPaymentIDDropsStaleDispensedCount(t *testing.T) {
	client := New(&config.Config{})
	client.recordDispensedCount("payment-123", 5)

	client.SetPaymentID("payment-456")

	if pending := client.pendingDispensedCount("payment-123"); pending != nil {
		t.Fatalf("expected stale count to be cleared, got %v", pending)
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

func TestCommandExecutionPolicy(t *testing.T) {
	client := New(&config.Config{})

	// Always-allowed commands
	for _, command := range []string{"message", "take_picture", "cancel"} {
		if !client.canExecuteCommandNow(&CommandResponse{Command: command}) {
			t.Fatalf("expected %q to be executable immediately", command)
		}
	}

	// Clean-state-only command should execute when clean
	client.setRuntimeState(StateIdle, "Idle")
	if !client.canExecuteCommandNow(&CommandResponse{Command: "load_test"}) {
		t.Fatal("expected load_test to be executable in idle state")
	}

	client.setRuntimeState(StateDetectingBall, "Warte auf Ball")
	if !client.canExecuteCommandNow(&CommandResponse{Command: "load_test"}) {
		t.Fatal("expected load_test to be executable in clean detecting_ball state")
	}

	// With an active payment, clean-state-only commands must be deferred
	client.SetPaymentID("payment-123")
	if client.canExecuteCommandNow(&CommandResponse{Command: "load_test"}) {
		t.Fatal("expected load_test to be deferred while payment is active")
	}

	// During active payment and waiting_for_amount, lightweight actuator commands are allowed.
	client.setRuntimeState(StateBallDetected, "Ball erkannt")
	client.setCurrentPayment("payment-123", map[string]any{"payment_phase": "waiting_for_amount"})
	if !client.canExecuteCommandNow(&CommandResponse{Command: "extend"}) {
		t.Fatal("expected extend to be executable while waiting_for_amount")
	}

	// During waiting_for_payment, actuator commands must be deferred.
	client.setCurrentPayment("payment-123", map[string]any{"payment_phase": "waiting_for_payment"})
	if client.canExecuteCommandNow(&CommandResponse{Command: "extend"}) {
		t.Fatal("expected extend to be deferred while waiting_for_payment")
	}
}

func TestActuatorLockPreventsRaceConditions(t *testing.T) {
	cfg := &config.Config{
		BaendaeliURL:     "http://example.com",
		BaendaeliAPIKey:  "test-key",
		ActuatorMovement: 1,
		ActuatorEnabled:  false,
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

func TestBallDispenserRecordsDispensedCount(t *testing.T) {
	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)
	client.SetPaymentID("payment-123")

	_, err := client.executeCommand(&CommandResponse{
		ID:      47,
		Command: "ball_dispenser",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	pending := client.pendingDispensedCount("payment-123")
	if pending == nil || *pending != 1 {
		t.Fatalf("expected pending count of 1, got %v", pending)
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
	_, err = client.executeCommand(cmd)
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

func TestCommandLoadTest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/commands") && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id": 48, "command": "load_test", "repeat_count": 9}`))
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

	if cmd.ID != 48 {
		t.Errorf("expected ID 48, got %d", cmd.ID)
	}

	if cmd.Command != "load_test" {
		t.Errorf("expected command 'load_test', got %s", cmd.Command)
	}

	if cmd.RepeatCount == nil {
		t.Fatal("expected repeat_count to be set")
	}

	if *cmd.RepeatCount != 9 {
		t.Errorf("expected repeat_count 9, got %d", *cmd.RepeatCount)
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
	_, err := client.executeCommand(cmd)
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

func TestWaitForBallReadyPassiveScanSetsJamStateAndMessage(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetDefaults()
	// Force failure quickly so we can assert stuck-funnel behavior deterministically.
	cfg.ColorSensorMovementThreshold = 10000
	cfg.ColorSensorCheckDurationMs = 1
	cfg.ColorSensorVibrateBursts = 0
	cfg.ColorSensorMaxAttempts = 1

	client := New(cfg)
	if err := client.colorSensor.Init(cfg); err != nil {
		t.Fatalf("failed to init color sensor in test: %v", err)
	}
	defer client.colorSensor.Close()

	err := client.waitForBallReady(true, false, nil)
	if err == nil {
		t.Fatal("expected waitForBallReady to fail")
	}
	if err != colorsensor.ErrNoBallDetected {
		t.Fatalf("expected ErrNoBallDetected, got %v", err)
	}
	if !client.jammed.Load() {
		t.Fatal("expected client to be jammed")
	}

	snapshot := client.GetStateSnapshot()
	if snapshot.State != string(StateBallStuckFunnel) {
		t.Fatalf("expected state %q, got %q", StateBallStuckFunnel, snapshot.State)
	}

	exec := client.GetExecutingCommand()
	if exec == nil {
		t.Fatal("expected executing command to contain stuck-funnel message")
	}
	if exec.Command != "message" {
		t.Fatalf("expected executing command 'message', got %q", exec.Command)
	}
	if exec.Message != "Ball steckt im Trichter. Rufe eine Techniker*in." {
		t.Fatalf("unexpected stuck-funnel message: %q", exec.Message)
	}
}

func TestWaitForBallReadyActiveScanSetsJamAfterMaxAttempts(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.ColorSensorMovementThreshold = 10000
	cfg.ColorSensorCheckDurationMs = 1
	cfg.ColorSensorVibrateBursts = 0
	cfg.ColorSensorMaxAttempts = 1

	client := New(cfg)
	if err := client.colorSensor.Init(cfg); err != nil {
		t.Fatalf("failed to init color sensor in test: %v", err)
	}
	defer client.colorSensor.Close()

	err := client.waitForBallReady(true, true, nil)
	if err != colorsensor.ErrNoBallDetected {
		t.Fatalf("expected ErrNoBallDetected after max attempts, got %v", err)
	}
	if !client.jammed.Load() {
		t.Fatal("expected jam state after active scan exhausted max attempts")
	}
}

func TestCaptureStartupBallReferenceBaselineClearsPendingWhenSensorDisabled(t *testing.T) {
	client := New(&config.Config{})
	baseline := uint16(123)
	client.setPendingBallReference(&baseline)

	client.captureStartupBallReferenceBaseline()

	if got := client.consumePendingBallReference(); got != nil {
		t.Fatalf("expected pending baseline to be cleared, got %v", *got)
	}
}

func TestWaitForBallReadyStoresReferenceBaselineForNextCycle(t *testing.T) {
	client := New(&config.Config{})
	reference := uint16(777)

	if err := client.waitForBallReady(false, false, &reference); err != nil {
		t.Fatalf("expected waitForBallReady to succeed, got %v", err)
	}

	next := client.consumePendingBallReference()
	if next == nil {
		t.Fatal("expected pending baseline to be stored")
	}
	if *next != reference {
		t.Fatalf("expected pending baseline %d, got %d", reference, *next)
	}
}

func TestPollSkipsCommandFetchWhenJammed(t *testing.T) {
	statusCalled := false
	commandCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/status") {
			statusCalled = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success": true}`))
			return
		}
		if strings.Contains(r.URL.Path, "/commands") {
			commandCalled = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"command":"extend","id":1}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.Config{BaendaeliURL: server.URL, BaendaeliAPIKey: "test-key"}
	cfg.SetDefaults()
	cfg.ColorSensorMovementThreshold = 10000
	cfg.ColorSensorCheckDurationMs = 1
	cfg.ColorSensorVibrateBursts = 0
	cfg.ColorSensorMaxAttempts = 1
	client := New(cfg)
	if err := client.colorSensor.Init(cfg); err != nil {
		t.Fatalf("failed to init color sensor in test: %v", err)
	}
	defer client.colorSensor.Close()
	client.jammed.Store(true)

	client.poll()

	if !statusCalled {
		t.Fatal("expected status endpoint to be called")
	}
	if commandCalled {
		t.Fatal("expected command endpoint to be skipped while jammed")
	}
}

func TestPollJammedRecoversAndFetchesCommand(t *testing.T) {
	statusCalled := false
	commandCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/status") {
			statusCalled = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success": true}`))
			return
		}
		if strings.Contains(r.URL.Path, "/commands") {
			commandCalled = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"command": null}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.Config{BaendaeliURL: server.URL, BaendaeliAPIKey: "test-key"}
	cfg.SetDefaults()
	cfg.ColorSensorMovementThreshold = 1
	cfg.ColorSensorCheckDurationMs = 400
	cfg.ColorSensorVibrateBursts = 0
	cfg.ColorSensorMaxAttempts = 1

	client := New(cfg)
	if err := client.colorSensor.Init(cfg); err != nil {
		t.Fatalf("failed to init color sensor in test: %v", err)
	}
	defer client.colorSensor.Close()
	client.jammed.Store(true)

	client.poll()

	if !statusCalled {
		t.Fatal("expected status endpoint to be called")
	}
	if !commandCalled {
		t.Fatal("expected command endpoint to be called after jam recovery")
	}
	if client.jammed.Load() {
		t.Fatal("expected jam flag to be cleared after passive detection")
	}
}

func TestGetStateSnapshotIncludesRuntimeFields(t *testing.T) {
	client := New(&config.Config{})
	client.SetPaymentID("payment-42")
	client.jammed.Store(true)
	client.setRuntimeState(StateAwaitingPayment, "Warten auf Zahlung")
	client.setExecutingCommand(&CommandResponse{Command: "message", Message: "Waiting"})
	client.setPendingCommand(&CommandResponse{Command: "load_test"})

	snapshot := client.GetStateSnapshot()

	if snapshot.State != string(StateAwaitingPayment) {
		t.Fatalf("expected state %q, got %q", StateAwaitingPayment, snapshot.State)
	}
	if snapshot.Message != "Warten auf Zahlung" {
		t.Fatalf("expected message to be preserved, got %q", snapshot.Message)
	}
	if snapshot.PaymentID != "payment-42" {
		t.Fatalf("expected payment_id payment-42, got %q", snapshot.PaymentID)
	}
	if !snapshot.Jammed {
		t.Fatal("expected jammed=true")
	}
	if snapshot.ExecutingCommand == nil || snapshot.ExecutingCommand.Command != "message" {
		t.Fatalf("expected executing command message, got %+v", snapshot.ExecutingCommand)
	}
	if snapshot.PendingCommand == nil || snapshot.PendingCommand.Command != "load_test" {
		t.Fatalf("expected pending command load_test, got %+v", snapshot.PendingCommand)
	}
}

func TestRunStateMachineCycleCreatesPaymentAndMovesToAwaiting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/api/v1/payment") {
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":"pay-100","qr_code_url":"https://example.com/qr/pay-100"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := New(&config.Config{BaendaeliURL: server.URL, BaendaeliAPIKey: "test-key"})

	handled := client.runStateMachineCycle()
	if !handled {
		t.Fatal("expected state machine cycle to handle payment creation path")
	}
	if got := client.GetPaymentID(); got != "pay-100" {
		t.Fatalf("expected payment id pay-100, got %q", got)
	}

	snapshot := client.GetStateSnapshot()
	if snapshot.State != string(StateBallDetected) {
		t.Fatalf("expected state ball_detected after payment creation, got %q", snapshot.State)
	}
	if snapshot.Payment == nil {
		t.Fatal("expected payment payload in snapshot")
	}
	if snapshot.Payment["qr_code_url"] != "https://example.com/qr/pay-100" {
		t.Fatalf("expected QR payload to be retained, got %+v", snapshot.Payment)
	}
}

func TestRunStateMachineCycleWaitingForAmountStaysBallDetected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/v1/payment/") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"waiting","payment_phase":"waiting_for_amount","amount_cents":null}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := New(&config.Config{BaendaeliURL: server.URL, BaendaeliAPIKey: "test-key"})
	client.setCurrentPayment("pay-200", map[string]any{"id": "pay-200", "qr_code_url": "https://example.com/qr/pay-200"})

	handled := client.runStateMachineCycle()
	if !handled {
		t.Fatal("expected waiting payment cycle to be handled")
	}

	snapshot := client.GetStateSnapshot()
	if snapshot.State != string(StateBallDetected) {
		t.Fatalf("expected state ball_detected while waiting for amount, got %q", snapshot.State)
	}
	if snapshot.ExecutingCommand != nil {
		t.Fatalf("expected no waiting message command while QR should remain visible, got %+v", snapshot.ExecutingCommand)
	}
	if snapshot.Payment == nil || snapshot.Payment["qr_code_url"] != "https://example.com/qr/pay-200" {
		t.Fatalf("expected payment QR payload to be retained, got %+v", snapshot.Payment)
	}
}

func TestRunStateMachineCycleWaitingForPaymentMovesToAwaitingPayment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/v1/payment/") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"waiting","payment_phase":"waiting_for_payment","amount_cents":2000}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := New(&config.Config{BaendaeliURL: server.URL, BaendaeliAPIKey: "test-key"})
	client.setCurrentPayment("pay-201", map[string]any{"id": "pay-201", "qr_code_url": "https://example.com/qr/pay-201"})

	handled := client.runStateMachineCycle()
	if !handled {
		t.Fatal("expected waiting-for-payment cycle to be handled")
	}

	snapshot := client.GetStateSnapshot()
	if snapshot.State != string(StateAwaitingPayment) {
		t.Fatalf("expected state awaiting_payment after amount selection, got %q", snapshot.State)
	}
	if snapshot.ExecutingCommand == nil || snapshot.ExecutingCommand.Message != "Warten auf Zahlung" {
		t.Fatalf("expected waiting message command, got %+v", snapshot.ExecutingCommand)
	}
	if snapshot.Payment == nil || snapshot.Payment["qr_code_url"] != "https://example.com/qr/pay-201" {
		t.Fatalf("expected payment QR payload to be retained, got %+v", snapshot.Payment)
	}
}

func TestRunStateMachineCyclePaymentFailureClearsPayment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/v1/payment/") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"failed"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := New(&config.Config{BaendaeliURL: server.URL, BaendaeliAPIKey: "test-key"})
	client.SetPaymentID("pay-300")

	handled := client.runStateMachineCycle()
	if !handled {
		t.Fatal("expected failed payment cycle to be handled")
	}
	if got := client.GetPaymentID(); got != "" {
		t.Fatalf("expected payment id to be cleared after failed payment, got %q", got)
	}

	snapshot := client.GetStateSnapshot()
	if snapshot.State != string(StateDetectingBall) {
		t.Fatalf("expected state detecting_ball after reset, got %q", snapshot.State)
	}
}

func TestRunStateMachineCyclePaymentSuccessDispensesAndResets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/v1/payment/") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := New(&config.Config{BaendaeliURL: server.URL, BaendaeliAPIKey: "test-key"})
	client.SetPaymentID("pay-400")

	handled := client.runStateMachineCycle()
	if !handled {
		t.Fatal("expected paid payment cycle to be handled")
	}
	if got := client.GetPaymentID(); got != "" {
		t.Fatalf("expected payment id to be cleared after dispense, got %q", got)
	}

	snapshot := client.GetStateSnapshot()
	if snapshot.State != string(StateDetectingBall) {
		t.Fatalf("expected state detecting_ball after successful dispense, got %q", snapshot.State)
	}
}

func TestStartSetsDetectingState(t *testing.T) {
	client := New(&config.Config{})
	client.Start()
	defer client.Stop()

	time.Sleep(20 * time.Millisecond)

	snapshot := client.GetStateSnapshot()
	if snapshot.State != string(StateDetectingBall) {
		t.Fatalf("expected start state detecting_ball, got %q", snapshot.State)
	}
}
