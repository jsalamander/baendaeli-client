# Actuator Calibration Guide

## Overview

The actuator system is designed to ensure **exactly equal forward and backward movement** using time-based control. This document explains how the system works and how to calibrate it properly.

## How Equal Movement is Ensured

### 1. **Identical Timing**
The system uses a **single** `ACTUATOR_MOVEMENT_SECONDS` value for both extend and retract operations:

```yaml
# config.yaml
ACTUATOR_MOVEMENT_SECONDS: 2  # Used for BOTH extend and retract
```

This guarantees the motor runs for exactly the same duration in both directions.

### 2. **Precise Timing**
- Uses `time.NewTimer` instead of `time.Sleep` for more accurate timing
- Timer-based delays are more resistant to system load variations

### 3. **Settling Delays**
- **100ms settling delay** added after each stop command
- Ensures motor fully stops before direction change
- Prevents momentum from carrying the actuator further than intended

### 4. **Known Home Position**
- System always homes (retracts fully) on startup
- Each trigger cycle starts from the known home position
- This eliminates accumulated positioning errors

### 5. **Position Tracking**
- Internal `isHome` flag tracks current state
- Logs warning if trigger is called when not at home
- Helps detect if system gets out of sync

## Calibration Procedure

### Step 1: Measure Full Travel Distance

1. Start with actuator fully retracted (home position)
2. Manually measure the maximum possible extension distance
3. Record this as your "full travel distance"

### Step 2: Determine Movement Time

Run the actuator with different timing values to find optimal duration:

```bash
# Edit config.yaml and test different values
ACTUATOR_MOVEMENT_SECONDS: 1  # Start conservative
# Test and observe actual travel distance
# Increase if you want more extension
ACTUATOR_MOVEMENT_SECONDS: 2  # Typical value
```

**Important:** 
- Start with shorter times and increase gradually
- Never exceed the actuator's physical limits
- The actuator will always retract fully during homing (10 seconds)

### Step 3: Verify Equal Movement

Test the actuator through multiple cycles:

```bash
# Start the server
./baendaeli-client

# Watch logs for:
# "Actuator: extending for exactly 2s..."
# "Actuator: retracting for exactly 2s (same as extend)..."
```

Check that:
1. Each extend reaches the same position
2. Each retract returns to home position
3. No position drift over multiple cycles

### Step 4: Mark Physical Positions

Once calibrated:
1. Mark the extended position with tape/marker
2. Verify actuator consistently reaches this mark
3. Document the calibrated value in your deployment notes

## Configuration Parameters

```yaml
# Required - must be identical for extend and retract
ACTUATOR_MOVEMENT_SECONDS: 2

# Pause time between extend and retract (user sees QR code)
ACTUATOR_PAUSE_SECONDS: 2

# GPIO pins (depends on your wiring)
ACTUATOR_ENA_PIN: "GPIO25"
ACTUATOR_IN1_PIN: "GPIO8"
ACTUATOR_IN2_PIN: "GPIO7"
```

## Troubleshooting

### Actuator Doesn't Return to Same Position

**Possible causes:**
1. **Voltage drop** - Check power supply capacity
   - Solution: Use dedicated 12V supply with adequate amperage
   
2. **Mechanical binding** - Check for obstructions
   - Solution: Lubricate rails, remove obstacles
   
3. **Motor wear** - Speed may have decreased
   - Solution: Recalibrate with slightly longer timing

### Actuator Extends/Retracts Different Distances

This should **not happen** with the current implementation because:
- Same timing value is used for both directions
- Code enforces `movementTime` for extend and retract
- Position tracking validates home state

If this occurs:
1. Check logs for warnings: `"Warning: actuator not at home position before trigger"`
2. Verify homing completes successfully on startup
3. Check for mechanical issues (binding, loose connections)

### Position Drifts Over Multiple Cycles

**Diagnosis:**
```bash
# Watch logs during multiple payment cycles
# Look for position warnings or timing variations
```

**Solutions:**
1. Increase settling delay if motor doesn't fully stop:
   ```go
   // In actuator.go
   const settlingDelay = 200 * time.Millisecond  // Increase from 100ms
   ```

2. Extend homing duration if not fully retracting:
   ```go
   // In actuator.go
   const homingDuration = 15 * time.Second  // Increase from 10s
   ```

3. Check for mechanical wear - may need maintenance

## Hardware Recommendations

For best accuracy:

1. **Power Supply**
   - Use regulated 12V supply
   - Ensure adequate amperage (typically 2-3A for actuators)
   - Separate from Raspberry Pi power if possible

2. **Actuator Selection**
   - Linear actuators with built-in limit switches (future upgrade)
   - Consistent speed rating across operating range
   - Quality brand with good mechanical design

3. **Mounting**
   - Rigid mounting to prevent flex
   - Smooth rails with minimal friction
   - Regular lubrication schedule

## Future Enhancements

For even better accuracy, consider:

1. **Hardware Limit Switches**
   - Install switches at both endpoints
   - Code stops when switch is triggered
   - Eliminates timing-based uncertainty

2. **Position Feedback**
   - Linear potentiometer for absolute position
   - Hall effect sensors for incremental positioning
   - Requires ADC and more complex code

3. **Current Sensing**
   - Monitor motor current
   - Detect when actuator stalls (reached limit)
   - More robust than time-based control

## Log Messages Reference

```
# Normal operation:
"Actuator config: movement_time=2s (extend=retract), pause=2s"
"Actuator initialized successfully (homing will run in background)"
"Actuator: retracting to home position..."
"Actuator: homing complete - now at home position"
"Actuator: extending for exactly 2s..."
"Actuator: retracting for exactly 2s (same as extend)..."
"Actuator cycle complete: extend=2s, retract=2s (identical), total=4234ms"

# Warnings:
"Warning: actuator not at home position before trigger"
"Actuator homing error: failed to set IN1 low: ..."
```

## Summary

The system ensures equal forward/backward movement through:
- ✅ Single timing value for both directions (enforced at config level)
- ✅ Precise timer-based delays (not Sleep)
- ✅ Settling delays between direction changes
- ✅ Always starting from known home position
- ✅ Position state tracking and validation

Follow the calibration procedure to find the optimal `ACTUATOR_MOVEMENT_SECONDS` value for your specific hardware setup.
