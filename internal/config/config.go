package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	BaendaeliAPIKey  string `yaml:"BAENDAELI_API_KEY"`
	BaendaeliURL     string `yaml:"BAENDAELI_URL"`
	DefaultAmount    int    `yaml:"DEFAULT_AMOUNT_CENTS"`
	SuccessOverlayMs int    `yaml:"SUCCESS_OVERLAY_MILLIS"`
	ActuatorEnabled  bool   `yaml:"ACTUATOR_ENABLED"`
	ActuatorENAPin   string `yaml:"ACTUATOR_ENA_PIN"`
	ActuatorIN1Pin   string `yaml:"ACTUATOR_IN1_PIN"`
	ActuatorIN2Pin   string `yaml:"ACTUATOR_IN2_PIN"`
	ActuatorExtend   int    `yaml:"ACTUATOR_EXTEND_SECONDS"`
	ActuatorRetract  int    `yaml:"ACTUATOR_RETRACT_SECONDS"`
	ActuatorPause    int    `yaml:"ACTUATOR_PAUSE_SECONDS"`
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
	if c.ActuatorExtend == 0 {
		c.ActuatorExtend = 2 // 2 seconds by default
	}
	if c.ActuatorRetract == 0 {
		c.ActuatorRetract = 2 // 2 seconds by default
	}
	if c.ActuatorPause == 0 {
		c.ActuatorPause = 2 // 2 seconds by default
	}
}
