package main

import (
"fmt"
"log"
"net/http"
"os"
"os/signal"
"syscall"

"github.com/jsalamander/baendaeli-client/internal/actuator"
"github.com/jsalamander/baendaeli-client/internal/config"
"github.com/jsalamander/baendaeli-client/internal/server"
)

func main() {
// Load configuration
cfg, err := config.Load("config.yaml")
if err != nil {
log.Fatalf("Failed to load config: %v", err)
}

// Apply defaults
cfg.SetDefaults()

// Initialize actuator if enabled
if cfg.ActuatorEnabled {
actuatorCfg := actuator.Config{
Enabled:      cfg.ActuatorEnabled,
ENAPin:       cfg.ActuatorENAPin,
IN1Pin:       cfg.ActuatorIN1Pin,
IN2Pin:       cfg.ActuatorIN2Pin,
MovementTime: cfg.ActuatorMovement,
PauseTime:    cfg.ActuatorPause,
}
if err := actuator.Init(actuatorCfg); err != nil {
log.Printf("Warning: Actuator initialization failed: %v. Continuing without actuator.", err)
}
defer actuator.Cleanup()
}

// Create server
srv := server.New(cfg)

// Setup graceful shutdown
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

// Start HTTP server in a goroutine
go func() {
addr := ":8000"
log.Printf("Starting server on %s", addr)
if err := http.ListenAndServe(addr, srv.Router()); err != nil && err != http.ErrServerClosed {
log.Fatalf("Server error: %v", err)
}
}()

	// Start actuator homing in background after server is up
	if cfg.ActuatorEnabled {
		go func() {
			log.Println("Starting actuator homing sequence...")
			actuator.Home()
		}()
	}

	// Wait for interrupt signal
	sig := <-sigChan
	fmt.Printf("\nReceived signal: %v. Shutting down...\n", sig)
}
