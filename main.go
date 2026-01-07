package main

import (
"fmt"
"log"
"net"
"net/http"
"os"
"os/signal"
"strconv"
"syscall"
"time"

"github.com/jsalamander/baendaeli-client/internal/actuator"
"github.com/jsalamander/baendaeli-client/internal/config"
"github.com/jsalamander/baendaeli-client/internal/server"
)

func main() {
// Check for subcommands
if len(os.Args) > 1 {
switch os.Args[1] {
case "extend":
runExtendCommand()
return
case "extract": // alias for extend (per request wording)
runExtendCommand()
return
case "retract":
runRetractCommand()
return
case "home":
runHomeCommand()
return
case "help", "-h", "--help":
printUsage()
return
default:
// Unknown subcommand: show usage and exit without starting server
printUsage()
return
}
}

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

// printUsage displays help information
func printUsage() {
	fmt.Println("Baendaeli Client - Payment QR Display System")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  baendaeli-client                    Start the web server (default)")
	fmt.Println("  baendaeli-client extend <ms>        Extend actuator for specified milliseconds")
	fmt.Println("  baendaeli-client extract <ms>       Alias for extend (same behavior)")
	fmt.Println("  baendaeli-client retract <ms>       Retract actuator for specified milliseconds")
	fmt.Println("  baendaeli-client home               Bring actuator to home position")
	fmt.Println("  baendaeli-client help               Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  baendaeli-client extend 2000        Extend for 2 seconds")
	fmt.Println("  baendaeli-client extract 2000       Extend for 2 seconds (alias)")
	fmt.Println("  baendaeli-client retract 1500       Retract for 1.5 seconds")
	fmt.Println("  baendaeli-client home               Retract fully to home position")
	fmt.Println()
	fmt.Println("Note: Actuator commands require ACTUATOR_ENABLED: true in config.yaml")
}

// runExtendCommand extends the actuator for specified milliseconds
func runExtendCommand() {
	if len(os.Args) < 3 {
		fmt.Println("Error: extend command requires duration in milliseconds")
		fmt.Println("Usage: baendaeli-client extend <ms>")
		fmt.Println("Example: baendaeli-client extend 2000")
		os.Exit(1)
	}

	ms, err := strconv.Atoi(os.Args[2])
	if err != nil || ms <= 0 {
		fmt.Printf("Error: invalid duration '%s'. Must be a positive integer (milliseconds)\n", os.Args[2])
		os.Exit(1)
	}

	printStopCommandsIfServerActive()

	duration := time.Duration(ms) * time.Millisecond
	fmt.Printf("Extending actuator for %v (%dms)...\n", duration, ms)

	if err := initActuatorForCommand(); err != nil {
		printStopCommandsIfServerActive()
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer actuator.Cleanup()

	if err := actuator.Extend(duration); err != nil {
		printStopCommandsIfServerActive()
		fmt.Printf("Error extending actuator: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Extend complete")
}

// runRetractCommand retracts the actuator for specified milliseconds
func runRetractCommand() {
	if len(os.Args) < 3 {
		fmt.Println("Error: retract command requires duration in milliseconds")
		fmt.Println("Usage: baendaeli-client retract <ms>")
		fmt.Println("Example: baendaeli-client retract 2000")
		os.Exit(1)
	}

	ms, err := strconv.Atoi(os.Args[2])
	if err != nil || ms <= 0 {
		fmt.Printf("Error: invalid duration '%s'. Must be a positive integer (milliseconds)\n", os.Args[2])
		os.Exit(1)
	}

	printStopCommandsIfServerActive()

	duration := time.Duration(ms) * time.Millisecond
	fmt.Printf("Retracting actuator for %v (%dms)...\n", duration, ms)

	if err := initActuatorForCommand(); err != nil {
		printStopCommandsIfServerActive()
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer actuator.Cleanup()

	if err := actuator.Retract(duration); err != nil {
		printStopCommandsIfServerActive()
		fmt.Printf("Error retracting actuator: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Retract complete")
}

// runHomeCommand brings the actuator to home position
func runHomeCommand() {
	fmt.Println("Bringing actuator to home position (full retraction)...")

	printStopCommandsIfServerActive()

	if err := initActuatorForCommand(); err != nil {
		printStopCommandsIfServerActive()
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer actuator.Cleanup()

	actuator.Home()
	fmt.Println("✓ Homing complete")
}

// initActuatorForCommand initializes the actuator for testing commands
func initActuatorForCommand() error {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !cfg.ActuatorEnabled {
		return fmt.Errorf("actuator is disabled in config.yaml. Set ACTUATOR_ENABLED: true to use actuator commands")
	}

	cfg.SetDefaults()

	actuatorCfg := actuator.Config{
		Enabled:      cfg.ActuatorEnabled,
		ENAPin:       cfg.ActuatorENAPin,
		IN1Pin:       cfg.ActuatorIN1Pin,
		IN2Pin:       cfg.ActuatorIN2Pin,
		MovementTime: cfg.ActuatorMovement,
		PauseTime:    cfg.ActuatorPause,
	}

	if err := actuator.Init(actuatorCfg); err != nil {
		return fmt.Errorf("actuator initialization failed: %w", err)
	}

	return nil
}

// printStopCommandsIfServerActive prints only the commands to stop services if port 8000 is in use
func printStopCommandsIfServerActive() {
	if isPortOpen("127.0.0.1:8000") || isPortOpen("[::1]:8000") {
		fmt.Println("sudo systemctl stop baendaeli-client.service")
		fmt.Println("sudo systemctl stop baendaeli-client-kiosk.service")
	}
}

func isPortOpen(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true
	}
	return false
}
