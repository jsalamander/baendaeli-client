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
        enabled:      false,
        movementTime: 10 * time.Millisecond,
        pause:        5 * time.Millisecond,
    }
    defer func() { actuator = prev }()

    start := time.Now()
    totalMs, err := Trigger()
    elapsed := time.Since(start)

    if err != nil {
        t.Fatalf("Trigger returned error: %v", err)
    }

    // expected ~2225ms (10ms extend + 1010ms retract + 5ms pause + 2x 100ms settling + 1000ms cooldown); allow buffer for scheduling
    if totalMs < 2100 || totalMs > 2500 {
        t.Fatalf("unexpected reported duration: %dms", totalMs)
    }
    if elapsed < 2100*time.Millisecond || elapsed > 2800*time.Millisecond {
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
