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
}

func TestSetDefaultsPreservesValues(t *testing.T) {
	cfg := &Config{
		DefaultAmount:     123,
		SuccessOverlayMs:  5000,
		ActuatorMovement:  3,
		ActuatorPause:     5,
	}
	cfg.SetDefaults()

	if cfg.DefaultAmount != 123 || cfg.SuccessOverlayMs != 5000 || cfg.ActuatorMovement != 3 || cfg.ActuatorPause != 5 {
		t.Fatalf("values should be preserved: %+v", cfg)
	}
}
