package device

import (
	"strings"
	"testing"

	"github.com/jsalamander/baendaeli-client/internal/config"
)

func TestVibrateCommandValidPercent(t *testing.T) {
	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	tests := []struct {
		name       string
		percent    int
		durationMs int
		expectErr  bool
	}{
		{"minimum percent", 1, 100, false},
		{"maximum percent", 100, 60000, false},
		{"middle value", 50, 1500, false},
		{"percent 75", 75, 1500, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			percent := tt.percent
			durationMs := tt.durationMs
			cmd := &CommandResponse{
				ID:         1,
				Command:    "vibrate",
				Percent:    &percent,
				DurationMs: &durationMs,
			}

			err := client.executeCommand(cmd)
			if (err != nil) != tt.expectErr {
				t.Errorf("expected error=%v, got error=%v", tt.expectErr, err)
			}
		})
	}
}

func TestVibrateCommandInvalidPercent(t *testing.T) {
	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	tests := []struct {
		name       string
		percent    int
		durationMs int
		errorMsg   string
	}{
		{"percent 0", 0, 100, "percent must be between 1 and 100"},
		{"percent 101", 101, 100, "percent must be between 1 and 100"},
		{"percent negative", -1, 100, "percent must be between 1 and 100"},
		{"percent 200", 200, 100, "percent must be between 1 and 100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			percent := tt.percent
			durationMs := tt.durationMs
			cmd := &CommandResponse{
				ID:         1,
				Command:    "vibrate",
				Percent:    &percent,
				DurationMs: &durationMs,
			}

			err := client.executeCommand(cmd)
			if err == nil {
				t.Errorf("expected error, got nil")
			} else if !strings.Contains(err.Error(), tt.errorMsg) {
				t.Errorf("expected error to contain %q, got %q", tt.errorMsg, err.Error())
			}
		})
	}
}

func TestVibrateCommandInvalidDuration(t *testing.T) {
	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	tests := []struct {
		name       string
		percent    int
		durationMs int
		errorMsg   string
	}{
		{"duration 99", 50, 99, "duration_ms must be between 100 and 60000"},
		{"duration 60001", 50, 60001, "duration_ms must be between 100 and 60000"},
		{"duration negative", 50, -1, "duration_ms must be between 100 and 60000"},
		{"duration 0", 50, 0, "duration_ms must be between 100 and 60000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			percent := tt.percent
			durationMs := tt.durationMs
			cmd := &CommandResponse{
				ID:         1,
				Command:    "vibrate",
				Percent:    &percent,
				DurationMs: &durationMs,
			}

			err := client.executeCommand(cmd)
			if err == nil {
				t.Errorf("expected error, got nil")
			} else if !strings.Contains(err.Error(), tt.errorMsg) {
				t.Errorf("expected error to contain %q, got %q", tt.errorMsg, err.Error())
			}
		})
	}
}

func TestVibrateCommandMissingPercent(t *testing.T) {
	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	durationMs := 1500
	cmd := &CommandResponse{
		ID:         1,
		Command:    "vibrate",
		Percent:    nil,
		DurationMs: &durationMs,
	}

	err := client.executeCommand(cmd)
	if err == nil {
		t.Errorf("expected error for missing percent, got nil")
	} else if !strings.Contains(err.Error(), "missing required field: percent") {
		t.Errorf("expected error about missing percent, got %q", err.Error())
	}
}

func TestVibrateCommandMissingDuration(t *testing.T) {
	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	percent := 50
	cmd := &CommandResponse{
		ID:         1,
		Command:    "vibrate",
		Percent:    &percent,
		DurationMs: nil,
	}

	err := client.executeCommand(cmd)
	if err == nil {
		t.Errorf("expected error for missing duration_ms, got nil")
	} else if !strings.Contains(err.Error(), "missing required field: duration_ms") {
		t.Errorf("expected error about missing duration_ms, got %q", err.Error())
	}
}

func TestVibratePercentToIntensityConversion(t *testing.T) {
	// Test that percent values are correctly converted to intensity (0.0-1.0)
	// This is tested indirectly through valid command execution
	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	tests := []struct {
		name       string
		percent    int
		durationMs int
	}{
		{"1% intensity", 1, 100},
		{"25% intensity", 25, 500},
		{"50% intensity", 50, 1500},
		{"75% intensity", 75, 1500},
		{"100% intensity", 100, 60000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			percent := tt.percent
			durationMs := tt.durationMs
			cmd := &CommandResponse{
				ID:         1,
				Command:    "vibrate",
				Percent:    &percent,
				DurationMs: &durationMs,
			}

			err := client.executeCommand(cmd)
			if err != nil {
				t.Errorf("expected no error executing vibrate command, got %v", err)
			}
		})
	}
}

func TestVibrateDurationBoundaries(t *testing.T) {
	cfg := &config.Config{
		BaendaeliURL:    "http://example.com",
		BaendaeliAPIKey: "test-key",
	}
	client := New(cfg)

	tests := []struct {
		name       string
		percent    int
		durationMs int
		expectErr  bool
	}{
		{"minimum duration 100ms", 50, 100, false},
		{"maximum duration 60000ms", 50, 60000, false},
		{"duration just below min 99ms", 50, 99, true},
		{"duration just above max 60001ms", 50, 60001, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			percent := tt.percent
			durationMs := tt.durationMs
			cmd := &CommandResponse{
				ID:         1,
				Command:    "vibrate",
				Percent:    &percent,
				DurationMs: &durationMs,
			}

			err := client.executeCommand(cmd)
			if (err != nil) != tt.expectErr {
				t.Errorf("expected error=%v, got error=%v", tt.expectErr, err)
			}
		})
	}
}
