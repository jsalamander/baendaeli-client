package main

import (
	"bufio"
	"bytes"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// Test isPortOpen() returns true when a TCP listener is active and false after closing.
func TestIsPortOpen(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on ephemeral port: %v", err)
	}
	addr := ln.Addr().String()

	if !isPortOpen(addr) {
		t.Fatalf("expected isPortOpen(%s) to be true while listener active", addr)
	}

	// Close and allow a brief moment for OS to release
	_ = ln.Close()
	time.Sleep(50 * time.Millisecond)

	if isPortOpen(addr) {
		t.Fatalf("expected isPortOpen(%s) to be false after listener closed", addr)
	}
}

// Test printStopCommandsIfServerActive prints commands when :8000 is open
func TestPrintStopCommandsIfServerActive(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:8000")
	if err != nil {
		t.Skip("port 8000 is already in use; skipping")
		return
	}
	defer ln.Close()

	// Capture stdout
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	printStopCommandsIfServerActive()

	// Restore stdout
	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("failed to read stdout: %v", err)
	}
	_ = r.Close()

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("sudo systemctl stop baendaeli-client.service")) {
		t.Fatalf("expected stop command for client service in output; got: %q", out)
	}
	if !bytes.Contains([]byte(out), []byte("sudo systemctl stop baendaeli-client-kiosk.service")) {
		t.Fatalf("expected stop command for kiosk service in output; got: %q", out)
	}
}

func TestWaitForCalibrationInputConfirm(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n"))
	action, err := waitForCalibrationInput(reader, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != calibrationInputConfirm {
		t.Fatalf("expected calibrationInputConfirm, got %v", action)
	}
}

func TestWaitForCalibrationInputRetryActuator(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("e\n"))
	action, err := waitForCalibrationInput(reader, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != calibrationInputRetryActuatorCycle {
		t.Fatalf("expected calibrationInputRetryActuatorCycle, got %v", action)
	}
}

func TestWaitForCalibrationInputRestartCycle(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("r\n"))
	action, err := waitForCalibrationInput(reader, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != calibrationInputRestartCycle {
		t.Fatalf("expected calibrationInputRestartCycle, got %v", action)
	}
}

func TestWaitForCalibrationInputRetryNotAllowedFallsBackToConfirm(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("e\n\n"))
	action, err := waitForCalibrationInput(reader, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != calibrationInputConfirm {
		t.Fatalf("expected calibrationInputConfirm after invalid input, got %v", action)
	}
}
