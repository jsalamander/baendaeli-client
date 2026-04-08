package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	BaendaeliAPIKey    string `yaml:"BAENDAELI_API_KEY"`
	BaendaeliURL       string `yaml:"BAENDAELI_URL"`
	DefaultAmount      int    `yaml:"DEFAULT_AMOUNT_CENTS"`
	SuccessOverlayMs   int    `yaml:"SUCCESS_OVERLAY_MILLIS"`
	ActuatorEnabled    bool   `yaml:"ACTUATOR_ENABLED"`
	ActuatorENAPin     string `yaml:"ACTUATOR_ENA_PIN"`
	ActuatorIN1Pin     string `yaml:"ACTUATOR_IN1_PIN"`
	ActuatorIN2Pin     string `yaml:"ACTUATOR_IN2_PIN"`
	ActuatorMovement   int    `yaml:"ACTUATOR_MOVEMENT_SECONDS"` // Used for both extend and retract
	ActuatorPause      int    `yaml:"ACTUATOR_PAUSE_SECONDS"`
	IRSensorEnabled    bool   `yaml:"IR_SENSOR_ENABLED"`
	IRSensor1Pin       string `yaml:"IR_SENSOR_1_PIN"`
	IRSensor2Pin       string `yaml:"IR_SENSOR_2_PIN"`
	IRSensorDebounceMs int    `yaml:"IR_SENSOR_DEBOUNCE_MS"`
	VibrationEnabled   bool   `yaml:"VIBRATOR_ENABLED"`
	VibrationIN3Pin    string `yaml:"VIBRATOR_IN3_PIN"`
	VibrationIN4Pin    string `yaml:"VIBRATOR_IN4_PIN"`
	VibrationENBPin    string `yaml:"VIBRATOR_ENB_PIN"`
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
	if c.ActuatorMovement == 0 {
		c.ActuatorMovement = 2 // 2 seconds by default (for both extend and retract)
	}
	if c.ActuatorPause == 0 {
		c.ActuatorPause = 2 // 2 seconds by default
	}
	if !c.IRSensorEnabled {
		c.IRSensorEnabled = true
	}
	if c.IRSensor1Pin == "" {
		c.IRSensor1Pin = "GPIO27"
	}
	if c.IRSensor2Pin == "" {
		c.IRSensor2Pin = "GPIO17"
	}
	if c.IRSensorDebounceMs == 0 {
		c.IRSensorDebounceMs = 10
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
}
