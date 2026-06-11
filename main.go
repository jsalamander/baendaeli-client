package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jsalamander/baendaeli-client/internal/actuator"
	"github.com/jsalamander/baendaeli-client/internal/camera"
	"github.com/jsalamander/baendaeli-client/internal/colorsensor"
	"github.com/jsalamander/baendaeli-client/internal/config"
	"github.com/jsalamander/baendaeli-client/internal/device"
	"github.com/jsalamander/baendaeli-client/internal/server"
	"github.com/jsalamander/baendaeli-client/internal/vibrator"
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
		case "color-debug":
			runColorDebugCommand()
			return
		case "state-calibrate", "measure-states":
			runStateCalibrationCommand()
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

	// Check camera tool availability at startup regardless of config
	camera.CheckTools()

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

	// Initialize vibrator if enabled
	if cfg.VibrationEnabled {
		vibCfg := vibrator.Config{
			Enabled: cfg.VibrationEnabled,
			IN3Pin:  cfg.VibrationIN3Pin,
			IN4Pin:  cfg.VibrationIN4Pin,
			ENBPin:  cfg.VibrationENBPin,
		}
		if err := vibrator.Init(vibCfg); err != nil {
			log.Printf("Warning: Vibrator initialization failed: %v. Continuing without vibrator.", err)
		}
		defer vibrator.Cleanup()
	}

	// Initialize camera if enabled
	if cfg.CameraEnabled {
		if err := camera.Init(camera.Config{Enabled: true}); err != nil {
			log.Printf("Warning: Camera initialization failed: %v. Continuing without camera.", err)
		}
		defer camera.Cleanup()
	}

	// Create server
	srv := server.New(cfg)

	// Create device client and set it on the server
	deviceClient := device.New(cfg)
	srv.SetDeviceClient(deviceClient)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start HTTP server in a goroutine
	go func() {
		addr := "0.0.0.0:8000"
		log.Printf("Starting server on %s", buildServerURL(addr))
		if err := http.ListenAndServe(addr, srv.Router()); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Start device client
	deviceClient.Start()

	// Wait for interrupt signal
	sig := <-sigChan
	fmt.Printf("\nReceived signal: %v. Shutting down...\n", sig)

	// Stop device client gracefully
	deviceClient.Stop()
}

func buildServerURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.HasPrefix(addr, ":") {
			port = strings.TrimPrefix(addr, ":")
			host = ""
		} else {
			return "http://" + addr
		}
	}

	if host == "0.0.0.0" || host == "::" {
		return fmt.Sprintf("http://%s:%s", host, port)
	}

	if host == "" {
		host = resolvePrimaryOutboundIP()
	}

	if host == "" {
		host = "localhost"
	}

	return fmt.Sprintf("http://%s:%s", host, port)
}

func resolvePrimaryOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()

	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || localAddr.IP == nil {
		return ""
	}

	return localAddr.IP.String()
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
	fmt.Println("  baendaeli-client color-debug [ms]   Print live TCS34725 C/R/G/B readings")
	fmt.Println("  baendaeli-client state-calibrate [n] Measure ball-present and manual-jam states")
	fmt.Println("  baendaeli-client help               Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  baendaeli-client extend 2000        Extend for 2 seconds")
	fmt.Println("  baendaeli-client extract 2000       Extend for 2 seconds (alias)")
	fmt.Println("  baendaeli-client retract 1500       Retract for 1.5 seconds")
	fmt.Println("  baendaeli-client home               Retract fully to home position")
	fmt.Println("  baendaeli-client color-debug 300    Print color values every 300ms")
	fmt.Println("  baendaeli-client state-calibrate 5  Measure 5 ball/jam state pairs")
	fmt.Println()
	fmt.Println("Note: Actuator commands require ACTUATOR_ENABLED: true in config.yaml")
}

// runColorDebugCommand prints live color sensor readings for threshold calibration.
func runColorDebugCommand() {
	interval := 500 * time.Millisecond
	if len(os.Args) >= 3 {
		ms, err := strconv.Atoi(os.Args[2])
		if err != nil || ms <= 0 {
			fmt.Printf("Error: invalid interval '%s'. Must be a positive integer (milliseconds)\n", os.Args[2])
			os.Exit(1)
		}
		interval = time.Duration(ms) * time.Millisecond
	}

	sensor, err := initColorSensorForCommand()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer sensor.Close()

	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Printf("Error: failed to load config.yaml: %v\n", err)
		os.Exit(1)
	}
	cfg.SetDefaults()
	cfg.ColorSensorEnabled = true

	fmt.Printf("Color debug started (interval=%v, threshold=%d)\n", interval, cfg.ColorSensorMovementThreshold)
	if sensor.IsSimulation() {
		fmt.Println("WARNING: color sensor is in SIMULATION mode (check I2C wiring/bus/address)")
	} else {
		fmt.Println("Color sensor is in HARDWARE mode")
	}
	fmt.Println("Press Ctrl+C to stop")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var baseline uint16
	for {
		c, r, g, b, err := sensor.Read()
		if err != nil {
			fmt.Printf("Read error: %v\n", err)
		} else {
			if baseline == 0 {
				baseline = c
			}
			delta := int(c) - int(baseline)
			if delta < 0 {
				delta = -delta
			}
			fmt.Printf("C:%5d R:%5d G:%5d B:%5d | dC:%4d baseline:%5d threshold:%d\n", c, r, g, b, delta, baseline, cfg.ColorSensorMovementThreshold)
		}

		<-ticker.C
	}
}

// runStateCalibrationCommand samples the ball-on-sensor and manual-jam states.
func runStateCalibrationCommand() {
	repeatCount := 5
	if len(os.Args) >= 3 {
		count, err := strconv.Atoi(os.Args[2])
		if err != nil || count <= 0 {
			fmt.Printf("Error: invalid repeat count '%s'. Must be a positive integer\n", os.Args[2])
			os.Exit(1)
		}
		repeatCount = count
	}

	printStopCommandsIfServerActive()

	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Printf("Error: failed to load config.yaml: %v\n", err)
		os.Exit(1)
	}
	cfg.SetDefaults()

	if !cfg.ActuatorEnabled {
		fmt.Println("Error: actuator is disabled in config.yaml. Set ACTUATOR_ENABLED: true to use state-calibrate")
		os.Exit(1)
	}

	cfg.ColorSensorEnabled = true
	sensor := colorsensor.New(cfg)
	if err := sensor.Init(cfg); err != nil {
		fmt.Printf("Error: failed to initialize color sensor: %v\n", err)
		os.Exit(1)
	}
	defer sensor.Close()

	if err := initActuatorForCommand(); err != nil {
		printStopCommandsIfServerActive()
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer actuator.Cleanup()

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("State calibration started (%d cycles)\n", repeatCount)
	fmt.Println("This helper records two labeled states: ball on the sensor and manual funnel jam.")
	if sensor.IsSimulation() {
		fmt.Println("WARNING: color sensor is in SIMULATION mode (check I2C wiring/bus/address)")
	} else {
		fmt.Println("Color sensor is in HARDWARE mode")
	}
	fmt.Println("For each cycle: place a ball on the sensor, press Enter, let the actuator run, create the jam manually, then press Enter again.")

	for cycle := 1; cycle <= repeatCount; cycle++ {
		fmt.Printf("\nCycle %d/%d - place a ball on the sensor and press Enter to record the ball-present state...", cycle, repeatCount)
		if err := waitForEnter(reader); err != nil {
			fmt.Printf("\nError waiting for confirmation: %v\n", err)
			os.Exit(1)
		}

		ballState, err := sampleRGBState(sensor, 3, 50*time.Millisecond)
		if err != nil {
			fmt.Printf("Error sampling ball-present state: %v\n", err)
			os.Exit(1)
		}
		logRGBState("ball-present", cycle, ballState)

		fmt.Printf("Cycle %d/%d - moving actuator to output the ball...\n", cycle, repeatCount)
		if _, err := actuator.Trigger(); err != nil {
			fmt.Printf("Error moving actuator: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Cycle %d/%d - create the funnel jam manually, then press Enter to record the jam state...", cycle, repeatCount)
		if err := waitForEnter(reader); err != nil {
			fmt.Printf("\nError waiting for jam confirmation: %v\n", err)
			os.Exit(1)
		}

		jamState, err := sampleRGBState(sensor, 3, 50*time.Millisecond)
		if err != nil {
			fmt.Printf("Error sampling jam state: %v\n", err)
			os.Exit(1)
		}
		logRGBState("jam", cycle, jamState)

		if cycle < repeatCount {
			fmt.Printf("Cycle %d/%d complete. Reset the setup and press Enter to continue...", cycle, repeatCount)
			if err := waitForEnter(reader); err != nil {
				fmt.Printf("\nError waiting for reset confirmation: %v\n", err)
				os.Exit(1)
			}
		}
	}

	fmt.Println("State calibration complete")
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

func initColorSensorForCommand() (*colorsensor.Sensor, error) {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	cfg.SetDefaults()
	cfg.ColorSensorEnabled = true

	sensor := colorsensor.New(cfg)
	if err := sensor.Init(cfg); err != nil {
		return nil, fmt.Errorf("failed to initialize color sensor: %w", err)
	}

	return sensor, nil
}

type rgbStateSample struct {
	C uint16
	R uint16
	G uint16
	B uint16
}

func sampleRGBState(sensor *colorsensor.Sensor, sampleCount int, interval time.Duration) (rgbStateSample, error) {
	if sampleCount < 1 {
		sampleCount = 1
	}

	var totalC, totalR, totalG, totalB uint64
	for i := 1; i <= sampleCount; i++ {
		c, r, g, b, err := sensor.Read()
		if err != nil {
			return rgbStateSample{}, err
		}
		totalC += uint64(c)
		totalR += uint64(r)
		totalG += uint64(g)
		totalB += uint64(b)
		fmt.Printf("  sample %d/%d: C:%5d R:%5d G:%5d B:%5d\n", i, sampleCount, c, r, g, b)
		if i < sampleCount {
			time.Sleep(interval)
		}
	}

	return rgbStateSample{
		C: uint16(totalC / uint64(sampleCount)),
		R: uint16(totalR / uint64(sampleCount)),
		G: uint16(totalG / uint64(sampleCount)),
		B: uint16(totalB / uint64(sampleCount)),
	}, nil
}

func logRGBState(label string, cycle int, sample rgbStateSample) {
	fmt.Printf("%s cycle %d average: C:%5d R:%5d G:%5d B:%5d\n", label, cycle, sample.C, sample.R, sample.G, sample.B)
}

func waitForEnter(reader *bufio.Reader) error {
	_, err := reader.ReadString('\n')
	return err
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
