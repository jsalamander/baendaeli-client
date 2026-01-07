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
    if cfg.ActuatorExtend != 2 || cfg.ActuatorRetract != 2 || cfg.ActuatorPause != 2 {
        t.Fatalf("Actuator defaults not set: %d %d %d", cfg.ActuatorExtend, cfg.ActuatorRetract, cfg.ActuatorPause)
    }
}

func TestSetDefaultsPreservesValues(t *testing.T) {
    cfg := &Config{
        DefaultAmount:    123,
        SuccessOverlayMs: 5000,
        ActuatorExtend:   3,
        ActuatorRetract:  4,
        ActuatorPause:    5,
    }
    cfg.SetDefaults()

    if cfg.DefaultAmount != 123 || cfg.SuccessOverlayMs != 5000 || cfg.ActuatorExtend != 3 || cfg.ActuatorRetract != 4 || cfg.ActuatorPause != 5 {
        t.Fatalf("values should be preserved: %+v", cfg)
    }
}
