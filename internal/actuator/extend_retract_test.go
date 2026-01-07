package actuator

import (
    "testing"
    "time"
)

func TestExtendReturnsErrorWhenUninitialized(t *testing.T) {
    prev := actuator
    actuator = nil
    defer func() { actuator = prev }()

    err := Extend(100 * time.Millisecond)
    if err == nil {
        t.Fatalf("expected error when actuator is nil")
    }
}

func TestRetractReturnsErrorWhenUninitialized(t *testing.T) {
    prev := actuator
    actuator = nil
    defer func() { actuator = prev }()

    err := Retract(100 * time.Millisecond)
    if err == nil {
        t.Fatalf("expected error when actuator is nil")
    }
}
