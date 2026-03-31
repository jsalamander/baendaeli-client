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
	if !cfg.IRSensorEnabled {
		t.Fatal("IRSensorEnabled default not set")
	}
	if cfg.IRSensor1Pin != "GPIO27" || cfg.IRSensor2Pin != "GPIO17" {
		t.Fatalf("IR sensor pin defaults not set: sensor1=%q sensor2=%q", cfg.IRSensor1Pin, cfg.IRSensor2Pin)
	}
	if cfg.IRSensorDebounceMs != 10 {
		t.Fatalf("IR sensor debounce default not set, got %d", cfg.IRSensorDebounceMs)
	}
}

func TestSetDefaultsPreservesValues(t *testing.T) {
	cfg := &Config{
		DefaultAmount:      123,
		SuccessOverlayMs:   5000,
		ActuatorMovement:   3,
		ActuatorPause:      5,
		IRSensorEnabled:    true,
		IRSensor1Pin:       "GPIO22",
		IRSensor2Pin:       "GPIO23",
		IRSensorDebounceMs: 25,
	}
	cfg.SetDefaults()

	if cfg.DefaultAmount != 123 || cfg.SuccessOverlayMs != 5000 || cfg.ActuatorMovement != 3 || cfg.ActuatorPause != 5 || !cfg.IRSensorEnabled || cfg.IRSensor1Pin != "GPIO22" || cfg.IRSensor2Pin != "GPIO23" || cfg.IRSensorDebounceMs != 25 {
		t.Fatalf("values should be preserved: %+v", cfg)
	}
}
