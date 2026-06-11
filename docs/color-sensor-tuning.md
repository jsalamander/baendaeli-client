# Color Sensor Detection Tuning Log

Hardware: TCS34725 (Purecrea, I2C 0x29/bus1), 2.8 cm transparent plastic baubles.

Detection mode: **hybrid-reference** — `hit = presence_hit || (movement_hit && c_guard_pass)`

---

## Config parameters

| Parameter | Description |
|---|---|
| `COLOR_SENSOR_MOVEMENT_THRESHOLD` | Min diff from current baseline to count as movement |
| `COLOR_SENSOR_PRESENCE_TOLERANCE` | Max diff from reference to count as presence |
| `COLOR_SENSOR_HYBRID_C_GUARD_MARGIN` | Minimum C above c_guard_floor for presence to count |
| `COLOR_SENSOR_REFERENCE_MAX_DRIFT` | Max drift before reference is temporarily ignored (movement-only fallback) |
| `COLOR_SENSOR_REFERENCE_RESAMPLE_AFTER_ATTEMPTS` | Hybrid reference resampled after N failed attempts |

---

## Observed C-value ranges

| Condition | C range |
|---|---|
| Ball present (transparent bauble) | ~580–730 |
| Jammed / partial | ~555–565 |
| Empty / post-dispense settling | ~540–560 |

Ball-vs-empty separation: ~25–35 units. Tight margin; c_guard is the main safeguard against empty-baseline detections.

---

## Iteration history

### log17–log20 (short runs, ~7 iterations each)
Goal: find optimal config for first-attempt detection rate.

Best result (log20):
- Attempt distribution: 5/7/0 (1st/2nd/3rd+) over 12 cycles
- 6 missed windows
- 0 third attempts

Config at log20:
```yaml
COLOR_SENSOR_MOVEMENT_THRESHOLD: 2
COLOR_SENSOR_PRESENCE_TOLERANCE: 24
COLOR_SENSOR_HYBRID_C_GUARD_MARGIN: 21
COLOR_SENSOR_REFERENCE_MAX_DRIFT: 35
COLOR_SENSOR_REFERENCE_RESAMPLE_AFTER_ATTEMPTS: 1
```

---

### log21 (50-iteration run, v0.21.22-ish)
Problem: 35 drift events vs. 7 in log20 (5× worse).

Root cause: post-dispense baseline shifts of 37–56 units exceeded `REFERENCE_MAX_DRIFT=35`, causing frequent movement-only fallback.

Fix: raised `REFERENCE_MAX_DRIFT` default 35 → **45**.

---

### log22 (50-iteration run, v0.21.23 — post-bugfix)

**Result: 50/50 balls dispensed, 0 false positives.**

Detection (33 visible cycles):
| Attempt | Count |
|---|---|
| 1st | 29 (87.9%) |
| 2nd | 2 |
| 3rd | 2 |

Drift events: 3 (all in first 5 cycles during initial C-value settle from ~622 → ~552).
Once settled, every remaining cycle was a clean 2-sample first-attempt detect.

C-value stabilised at **~551–552** for the bulk of the run.

Config at log22 (v0.21.23):
```yaml
COLOR_SENSOR_MOVEMENT_THRESHOLD: 2
COLOR_SENSOR_PRESENCE_TOLERANCE: 24
COLOR_SENSOR_HYBRID_C_GUARD_MARGIN: 21
COLOR_SENSOR_REFERENCE_MAX_DRIFT: 45
COLOR_SENSOR_REFERENCE_RESAMPLE_AFTER_ATTEMPTS: 1
```

---

## Known bugs fixed

### Empty-baseline false positive loop (fixed in 8c17e4b, v0.21.23)

**Symptom:** System continuously detects balls and dispenses with nothing in the tube.

**Root cause:** When reference drift forced movement-only mode, the old code still executed the hybrid reference resample on a miss. If the miss happened on an empty tube, the resampled reference matched the empty state. Subsequent attempts then matched via `presence_hit=true` indefinitely.

**Fix (`internal/colorsensor/monitor.go`):** When `forceMovementOnly=true`, skip the resample entirely — only reset `failedReferenceAttempts=0`. The `forceMovementOnly` flag already signals "reference is untrusted"; resampling under that condition was the contradiction.

---

## Next tuning directions

- The 3 drift events in log22 all occurred in the first ~5 cycles during initial C-value drift (622→552, delta up to 128). Consider whether the startup reference baseline capture should use a settling period or average multiple samples.
- `PRESENCE_TOLERANCE=24` with empty C≈552 and ball C≈580+ gives ~28 units headroom. Tightening c_guard_margin or raising presence_tolerance could help if false positives reappear.
- `REFERENCE_RESAMPLE_AFTER_ATTEMPTS=1` means resample on every single miss — evaluate whether 2 would reduce noise-triggered resamples without hurting recovery speed.
