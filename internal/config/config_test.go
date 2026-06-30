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
	if cfg.HTTPRequestLogging {
		t.Fatal("HTTPRequestLogging should be disabled by default")
	}
	if !cfg.LogShippingEnabled {
		t.Fatal("LogShippingEnabled default not set")
	}
	if cfg.LogShippingFlushIntervalMs != 3000 {
		t.Fatalf("LogShippingFlushIntervalMs default not set, got %d", cfg.LogShippingFlushIntervalMs)
	}
	if cfg.LogShippingBatchLines != 200 {
		t.Fatalf("LogShippingBatchLines default not set, got %d", cfg.LogShippingBatchLines)
	}
	if cfg.LogShippingMaxQueueLines != 5000 {
		t.Fatalf("LogShippingMaxQueueLines default not set, got %d", cfg.LogShippingMaxQueueLines)
	}
	if cfg.LogShippingMaxLineBytes != 16384 {
		t.Fatalf("LogShippingMaxLineBytes default not set, got %d", cfg.LogShippingMaxLineBytes)
	}
	if cfg.LogShippingMaxRequestBytes != 262144 {
		t.Fatalf("LogShippingMaxRequestBytes default not set, got %d", cfg.LogShippingMaxRequestBytes)
	}
	if cfg.ActuatorMovement != 2 || cfg.ActuatorPause != 0 {
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
	if !cfg.ColorSensorClearBandEnabled {
		t.Fatal("ColorSensorClearBandEnabled default not set")
	}
	if cfg.ColorSensorClearJamMax != 584 {
		t.Fatalf("ColorSensorClearJamMax default not set, got %d", cfg.ColorSensorClearJamMax)
	}
	if cfg.ColorSensorClearBallMin != 592 {
		t.Fatalf("ColorSensorClearBallMin default not set, got %d", cfg.ColorSensorClearBallMin)
	}
	if cfg.ColorSensorClearBandWindowMs != 400 {
		t.Fatalf("ColorSensorClearBandWindowMs default not set, got %d", cfg.ColorSensorClearBandWindowMs)
	}
	if cfg.ColorSensorPresenceTolerance != 18 {
		t.Fatalf("ColorSensorPresenceTolerance default not set, got %d", cfg.ColorSensorPresenceTolerance)
	}
	if cfg.ColorSensorReferenceMaxDrift != 45 {
		t.Fatalf("ColorSensorReferenceMaxDrift default not set, got %d", cfg.ColorSensorReferenceMaxDrift)
	}
	if cfg.ColorSensorReferenceResampleAfterAttempts != 2 {
		t.Fatalf("ColorSensorReferenceResampleAfterAttempts default not set, got %d", cfg.ColorSensorReferenceResampleAfterAttempts)
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
	if cfg.BreakBeamPin != "GPIO10" {
		t.Fatalf("BreakBeamPin default not set, got %q", cfg.BreakBeamPin)
	}
	if cfg.BreakBeamPollIntervalMs != 10 {
		t.Fatalf("BreakBeamPollIntervalMs default not set, got %d", cfg.BreakBeamPollIntervalMs)
	}
}

func TestSetDefaultsPreservesValues(t *testing.T) {
	cfg := &Config{
		DefaultAmount:                             123,
		SuccessOverlayMs:                          5000,
		HTTPRequestLogging:                        true,
		LogShippingEnabled:                        true,
		LogShippingFlushIntervalMs:                1234,
		LogShippingBatchLines:                     55,
		LogShippingMaxQueueLines:                  777,
		LogShippingMaxLineBytes:                   4444,
		LogShippingMaxRequestBytes:                99999,
		ActuatorMovement:                          3,
		ActuatorPause:                             5,
		ColorSensorEnabled:                        true,
		ColorSensorI2CBus:                         3,
		ColorSensorI2CAddress:                     "0x30",
		ColorSensorMovementThreshold:              1000,
		ColorSensorClearBandEnabled:               true,
		ColorSensorClearJamMax:                    510,
		ColorSensorClearBallMin:                   540,
		ColorSensorClearBandWindowMs:              320,
		ColorSensorPresenceTolerance:              7,
		ColorSensorReferenceMaxDrift:              44,
		ColorSensorReferenceResampleAfterAttempts: 3,
		ColorSensorPollIntervalMs:                 250,
		ColorSensorStableSamples:                  3,
		ColorSensorSettleDelayMs:                  350,
		ColorSensorDebugLogging:                   true,
		BreakBeamEnabled:                          true,
		BreakBeamPin:                              "GPIO11",
		BreakBeamPollIntervalMs:                   6,
		BreakBeamDebugLogging:                     true,
	}
	cfg.SetDefaults()

	if cfg.DefaultAmount != 123 || cfg.SuccessOverlayMs != 5000 || !cfg.HTTPRequestLogging || !cfg.LogShippingEnabled || cfg.LogShippingFlushIntervalMs != 1234 || cfg.LogShippingBatchLines != 55 || cfg.LogShippingMaxQueueLines != 777 || cfg.LogShippingMaxLineBytes != 4444 || cfg.LogShippingMaxRequestBytes != 99999 || cfg.ActuatorMovement != 3 || cfg.ActuatorPause != 5 || !cfg.ColorSensorEnabled || cfg.ColorSensorI2CBus != 3 || cfg.ColorSensorI2CAddress != "0x30" || cfg.ColorSensorMovementThreshold != 1000 || !cfg.ColorSensorClearBandEnabled || cfg.ColorSensorClearJamMax != 510 || cfg.ColorSensorClearBallMin != 540 || cfg.ColorSensorClearBandWindowMs != 320 || cfg.ColorSensorPresenceTolerance != 7 || cfg.ColorSensorReferenceMaxDrift != 44 || cfg.ColorSensorReferenceResampleAfterAttempts != 3 || cfg.ColorSensorPollIntervalMs != 250 || cfg.ColorSensorStableSamples != 3 || cfg.ColorSensorSettleDelayMs != 350 || !cfg.ColorSensorDebugLogging || !cfg.BreakBeamEnabled || cfg.BreakBeamPin != "GPIO11" || cfg.BreakBeamPollIntervalMs != 6 || !cfg.BreakBeamDebugLogging {
		t.Fatalf("values should be preserved: %+v", cfg)
	}
}
