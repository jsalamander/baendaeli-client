package actuator

import (
    "testing"
    "time"
)

// ensure Init is a no-op when disabled and does not set the global actuator
func TestInitDisabledLeavesActuatorNil(t *testing.T) {
    prev := actuator
    actuator = nil
    defer func() { actuator = prev }()

    cfg := Config{Enabled: false}
    if err := Init(cfg); err != nil {
        t.Fatalf("Init returned error for disabled config: %v", err)
    }
    if actuator != nil {
        t.Fatalf("actuator should remain nil when disabled")
    }
}

// validate Trigger uses mock timing path when actuator is disabled
func TestTriggerWithDisabledActuatorUsesConfiguredDurations(t *testing.T) {
    prev := actuator
    actuator = &Actuator{
        enabled: false,
        extend:  10 * time.Millisecond,
        pause:   5 * time.Millisecond,
        retract: 10 * time.Millisecond,
    }
    defer func() { actuator = prev }()

    start := time.Now()
    totalMs, err := Trigger()
    elapsed := time.Since(start)

    if err != nil {
        t.Fatalf("Trigger returned error: %v", err)
    }

    // expected ~25ms; allow a small buffer for scheduling jitter
    if totalMs < 15 || totalMs > 200 {
        t.Fatalf("unexpected reported duration: %dms", totalMs)
    }
    if elapsed < 15*time.Millisecond || elapsed > 300*time.Millisecond {
        t.Fatalf("unexpected elapsed wall time: %v", elapsed)
    }
}

// ensure Cleanup tolerates nil actuator without panicking
func TestCleanupNilSafe(t *testing.T) {
    prev := actuator
    actuator = nil
    defer func() { actuator = prev }()

    Cleanup() // should not panic
}
