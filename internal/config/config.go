package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	BaendaeliAPIKey                           string  `yaml:"BAENDAELI_API_KEY"`
	BaendaeliURL                              string  `yaml:"BAENDAELI_URL"`
	HTTPRequestLogging                        bool    `yaml:"HTTP_REQUEST_LOGGING"`
	DefaultAmount                             int     `yaml:"DEFAULT_AMOUNT_CENTS"`
	SuccessOverlayMs                          int     `yaml:"SUCCESS_OVERLAY_MILLIS"`
	ActuatorEnabled                           bool    `yaml:"ACTUATOR_ENABLED"`
	ActuatorENAPin                            string  `yaml:"ACTUATOR_ENA_PIN"`
	ActuatorIN1Pin                            string  `yaml:"ACTUATOR_IN1_PIN"`
	ActuatorIN2Pin                            string  `yaml:"ACTUATOR_IN2_PIN"`
	ActuatorMovement                          int     `yaml:"ACTUATOR_MOVEMENT_SECONDS"` // Used for both extend and retract
	ActuatorPause                             int     `yaml:"ACTUATOR_PAUSE_SECONDS"`
	ColorSensorEnabled                        bool    `yaml:"COLOR_SENSOR_ENABLED"`
	ColorSensorI2CBus                         int     `yaml:"COLOR_SENSOR_I2C_BUS"`
	ColorSensorI2CAddress                     string  `yaml:"COLOR_SENSOR_I2C_ADDRESS"`
	ColorSensorMovementThreshold              int     `yaml:"COLOR_SENSOR_MOVEMENT_THRESHOLD"`
	ColorSensorPresenceTolerance              int     `yaml:"COLOR_SENSOR_PRESENCE_TOLERANCE"`
	ColorSensorHybridCGuardMargin             int     `yaml:"COLOR_SENSOR_HYBRID_C_GUARD_MARGIN"`
	ColorSensorReferenceMaxDrift              int     `yaml:"COLOR_SENSOR_REFERENCE_MAX_DRIFT"`
	ColorSensorReferenceResampleAfterAttempts int     `yaml:"COLOR_SENSOR_REFERENCE_RESAMPLE_AFTER_ATTEMPTS"`
	ColorSensorPollIntervalMs                 int     `yaml:"COLOR_SENSOR_POLL_INTERVAL_MS"`
	ColorSensorCheckDurationMs                int     `yaml:"COLOR_SENSOR_CHECK_DURATION_MS"`
	ColorSensorStableSamples                  int     `yaml:"COLOR_SENSOR_STABLE_SAMPLES"`
	ColorSensorSettleDelayMs                  int     `yaml:"COLOR_SENSOR_SETTLE_DELAY_MS"`
	ColorSensorDebugLogging                   bool    `yaml:"COLOR_SENSOR_DEBUG_LOGGING"`
	ColorSensorVibrateIntensity               float64 `yaml:"COLOR_SENSOR_VIBRATE_INTENSITY"`
	ColorSensorVibrateDurationMs              int     `yaml:"COLOR_SENSOR_VIBRATE_DURATION_MS"`
	ColorSensorVibrateBursts                  int     `yaml:"COLOR_SENSOR_VIBRATE_BURSTS"`
	ColorSensorMaxAttempts                    int     `yaml:"COLOR_SENSOR_MAX_ATTEMPTS"`
	VibrationEnabled                          bool    `yaml:"VIBRATOR_ENABLED"`
	VibrationIN3Pin                           string  `yaml:"VIBRATOR_IN3_PIN"`
	VibrationIN4Pin                           string  `yaml:"VIBRATOR_IN4_PIN"`
	VibrationENBPin                           string  `yaml:"VIBRATOR_ENB_PIN"`
	CameraEnabled                             bool    `yaml:"CAMERA_ENABLED"`
}

func Load(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

func (c *Config) SetDefaults() {
	if c.DefaultAmount == 0 {
		c.DefaultAmount = 2000 // default to 20.00 CHF
	}
	if c.SuccessOverlayMs == 0 {
		c.SuccessOverlayMs = 10000 // 10 seconds by default
	}
	// HTTPRequestLogging defaults to false to reduce browser request log noise.
	if c.ActuatorMovement == 0 {
		c.ActuatorMovement = 2 // 2 seconds by default (for both extend and retract)
	}
	// ActuatorPause is intentionally left at 0 (deprecated/ignored by actuator trigger cycle).
	if !c.ColorSensorEnabled {
		c.ColorSensorEnabled = true
	}
	if c.ColorSensorI2CBus == 0 {
		c.ColorSensorI2CBus = 1
	}
	if c.ColorSensorI2CAddress == "" {
		c.ColorSensorI2CAddress = "0x29"
	}
	if c.ColorSensorMovementThreshold == 0 {
		c.ColorSensorMovementThreshold = 500
	}
	if c.ColorSensorPresenceTolerance == 0 {
		c.ColorSensorPresenceTolerance = 18
	}
	if c.ColorSensorHybridCGuardMargin == 0 {
		c.ColorSensorHybridCGuardMargin = 24
	}
	if c.ColorSensorReferenceMaxDrift == 0 {
		c.ColorSensorReferenceMaxDrift = 45
	}
	if c.ColorSensorReferenceResampleAfterAttempts == 0 {
		c.ColorSensorReferenceResampleAfterAttempts = 2
	}
	if c.ColorSensorPollIntervalMs == 0 {
		c.ColorSensorPollIntervalMs = 100
	}
	if c.ColorSensorCheckDurationMs == 0 {
		c.ColorSensorCheckDurationMs = 5000
	}
	if c.ColorSensorStableSamples == 0 {
		c.ColorSensorStableSamples = 2
	}
	if c.ColorSensorSettleDelayMs == 0 {
		c.ColorSensorSettleDelayMs = 200
	}
	if c.ColorSensorVibrateIntensity == 0 {
		c.ColorSensorVibrateIntensity = 0.8
	}
	if c.ColorSensorVibrateDurationMs == 0 {
		c.ColorSensorVibrateDurationMs = 400
	}
	if c.ColorSensorVibrateBursts == 0 {
		c.ColorSensorVibrateBursts = 3
	}
	if c.ColorSensorMaxAttempts == 0 {
		c.ColorSensorMaxAttempts = 5
	}
	if c.VibrationIN3Pin == "" {
		c.VibrationIN3Pin = "GPIO16"
	}
	if c.VibrationIN4Pin == "" {
		c.VibrationIN4Pin = "GPIO20"
	}
	if c.VibrationENBPin == "" {
		c.VibrationENBPin = "GPIO18"
	}
	if !c.CameraEnabled {
		c.CameraEnabled = true
	}
}
