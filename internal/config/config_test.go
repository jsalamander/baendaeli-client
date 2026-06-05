package config

import "testing"

func TestSetDefaultsAppliesValues(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()

	if cfg.DefaultAmount != 2000 {
		t.Fatalf("DefaultAmount not set, got %d", cfg.DefaultAmount)
	}
	if cfg.SuccessOverlayMs != 10000 {
		t.Fatalf("SuccessOverlayMs not set, got %d", cfg.SuccessOverlayMs)
	}
	if cfg.ActuatorMovement != 2 || cfg.ActuatorPause != 2 {
		t.Fatalf("Actuator defaults not set: movement=%d pause=%d", cfg.ActuatorMovement, cfg.ActuatorPause)
	}
	if !cfg.ColorSensorEnabled {
		t.Fatal("ColorSensorEnabled default not set")
	}
	if cfg.ColorSensorI2CBus != 1 {
		t.Fatalf("ColorSensorI2CBus default not set, got %d", cfg.ColorSensorI2CBus)
	}
	if cfg.ColorSensorI2CAddress != "0x29" {
		t.Fatalf("ColorSensorI2CAddress default not set, got %q", cfg.ColorSensorI2CAddress)
	}
	if cfg.ColorSensorMovementThreshold != 500 {
		t.Fatalf("ColorSensorMovementThreshold default not set, got %d", cfg.ColorSensorMovementThreshold)
	}
	if cfg.ColorSensorPresenceTolerance != 12 {
		t.Fatalf("ColorSensorPresenceTolerance default not set, got %d", cfg.ColorSensorPresenceTolerance)
	}
	if cfg.ColorSensorPollIntervalMs != 100 {
		t.Fatalf("ColorSensorPollIntervalMs default not set, got %d", cfg.ColorSensorPollIntervalMs)
	}
	if cfg.ColorSensorStableSamples != 2 {
		t.Fatalf("ColorSensorStableSamples default not set, got %d", cfg.ColorSensorStableSamples)
	}
	if cfg.ColorSensorSettleDelayMs != 200 {
		t.Fatalf("ColorSensorSettleDelayMs default not set, got %d", cfg.ColorSensorSettleDelayMs)
	}
}

func TestSetDefaultsPreservesValues(t *testing.T) {
	cfg := &Config{
		DefaultAmount:                123,
		SuccessOverlayMs:             5000,
		ActuatorMovement:             3,
		ActuatorPause:                5,
		ColorSensorEnabled:           true,
		ColorSensorI2CBus:            3,
		ColorSensorI2CAddress:        "0x30",
		ColorSensorMovementThreshold: 1000,
		ColorSensorPresenceTolerance: 7,
		ColorSensorPollIntervalMs:    250,
		ColorSensorStableSamples:     3,
		ColorSensorSettleDelayMs:     350,
		ColorSensorDebugLogging:      true,
	}
	cfg.SetDefaults()

	if cfg.DefaultAmount != 123 || cfg.SuccessOverlayMs != 5000 || cfg.ActuatorMovement != 3 || cfg.ActuatorPause != 5 || !cfg.ColorSensorEnabled || cfg.ColorSensorI2CBus != 3 || cfg.ColorSensorI2CAddress != "0x30" || cfg.ColorSensorMovementThreshold != 1000 || cfg.ColorSensorPresenceTolerance != 7 || cfg.ColorSensorPollIntervalMs != 250 || cfg.ColorSensorStableSamples != 3 || cfg.ColorSensorSettleDelayMs != 350 || !cfg.ColorSensorDebugLogging {
		t.Fatalf("values should be preserved: %+v", cfg)
	}
}
