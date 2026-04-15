#!/usr/bin/env python3
"""
ADS1115 Ball-Impact Calibration Script
=======================================
Measures the ADS1115 ADC (A0) before and after a ball drops into the holder,
repeats for a configurable number of rounds, and recommends a detection threshold.

Hardware: ADS1115 @ 0x48 on I2C-1, single-ended A0 vs GND, PGA +/-4.096V.

Usage:
    pip install smbus2
    python3 scripts/calibrate_pressure.py [--rounds N] [--samples N] [--bus N] [--addr 0xHH]

Example:
    python3 scripts/calibrate_pressure.py --rounds 5 --samples 10
"""

import argparse
import statistics
import sys
import time

try:
    from smbus2 import SMBus
except ImportError:
    print("ERROR: smbus2 is not installed.  Run:  pip install smbus2", file=sys.stderr)
    sys.exit(1)


# ---------------------------------------------------------------------------
# ADS1115 constants (must match internal/pressure/sensor.go)
# ---------------------------------------------------------------------------
REG_CONVERSION = 0x00
REG_CONFIG     = 0x01

OS_SINGLE   = 0x8000   # Start a single-shot conversion
MUX_A0_GND  = 0x4000   # Single-ended A0 vs GND
PGA_4_096V  = 0x0200   # +/-4.096V  →  1 LSB = 125 µV
MODE_SINGLE = 0x0100   # Power-down after conversion
DR_128SPS   = 0x0080   # 128 samples per second
COMP_DISABLE = 0x0003  # Disable comparator

CONFIG = OS_SINGLE | MUX_A0_GND | PGA_4_096V | MODE_SINGLE | DR_128SPS | COMP_DISABLE

VOLTS_PER_LSB = 4.096 / 32768  # 0.000125 V


# ---------------------------------------------------------------------------
# Low-level ADS1115 helpers
# ---------------------------------------------------------------------------

def write_config(bus: SMBus, addr: int) -> None:
    """Trigger a single-shot conversion on A0."""
    high = (CONFIG >> 8) & 0xFF
    low  =  CONFIG       & 0xFF
    bus.write_i2c_block_data(addr, REG_CONFIG, [high, low])


def read_conversion(bus: SMBus, addr: int) -> int:
    """Return the signed 16-bit raw ADC result."""
    data = bus.read_i2c_block_data(addr, REG_CONVERSION, 2)
    raw  = (data[0] << 8) | data[1]
    if raw & 0x8000:          # two's-complement for negative values
        raw -= (1 << 16)
    return raw


def raw_to_volts(raw: int) -> float:
    return raw * VOLTS_PER_LSB


def single_shot(bus: SMBus, addr: int) -> tuple[int, float]:
    """Perform one single-shot measurement. Returns (raw, volts)."""
    write_config(bus, addr)
    time.sleep(0.010)         # 10 ms is safe at 128 SPS
    raw = read_conversion(bus, addr)
    return raw, raw_to_volts(raw)


# ---------------------------------------------------------------------------
# Sampling helpers
# ---------------------------------------------------------------------------

def take_samples(bus: SMBus, addr: int, n: int, label: str) -> list[float]:
    """Collect *n* consecutive voltage readings, printing each one."""
    volts_list = []
    for i in range(n):
        raw, v = single_shot(bus, addr)
        print(f"  [{label}] sample {i+1:>2}/{n}  raw={raw:6d}  V={v:+.4f}")
        volts_list.append(v)
        time.sleep(0.050)     # 50 ms between samples (≈ 20 Hz effective rate)
    return volts_list


def burst_peak(bus: SMBus, addr: int, duration_s: float = 0.5) -> tuple[float, list[float]]:
    """
    Continuously sample for *duration_s* seconds and return the peak voltage
    together with all samples collected during the burst.
    """
    end = time.monotonic() + duration_s
    readings = []
    while time.monotonic() < end:
        raw, v = single_shot(bus, addr)
        readings.append(v)
    peak = max(readings)
    return peak, readings


def stats(values: list[float]) -> dict:
    if len(values) == 0:
        return {}
    return {
        "n":      len(values),
        "min":    min(values),
        "max":    max(values),
        "mean":   statistics.mean(values),
        "stdev":  statistics.stdev(values) if len(values) > 1 else 0.0,
    }


def fmt_stats(s: dict) -> str:
    return (
        f"n={s['n']}  min={s['min']:+.4f}V  max={s['max']:+.4f}V  "
        f"mean={s['mean']:+.4f}V  stdev={s['stdev']:.4f}V"
    )


# ---------------------------------------------------------------------------
# Main calibration loop
# ---------------------------------------------------------------------------

def calibrate(bus_num: int, addr: int, rounds: int, baseline_samples: int) -> None:
    print(f"\n=== ADS1115 Ball-Impact Calibration ===")
    print(f"  I2C bus : {bus_num}  (device /dev/i2c-{bus_num})")
    print(f"  Address : 0x{addr:02X}")
    print(f"  Rounds  : {rounds}")
    print(f"  Baseline samples per round: {baseline_samples}")
    print()

    all_baseline: list[float] = []
    all_peaks:    list[float] = []

    with SMBus(bus_num) as bus:
        for rnd in range(1, rounds + 1):
            print(f"── Round {rnd}/{rounds} " + "─" * 40)

            # ── 1. Baseline (holder empty, no ball) ──────────────────────────
            input(f"  [Round {rnd}] Make sure the holder is EMPTY, then press Enter…")
            print(f"  Measuring baseline ({baseline_samples} samples)…")
            baseline = take_samples(bus, addr, baseline_samples, "baseline")
            all_baseline.extend(baseline)
            bs = stats(baseline)
            print(f"  Baseline: {fmt_stats(bs)}")
            print()

            # ── 2. Impact (drop the ball) ────────────────────────────────────
            input(f"  [Round {rnd}] Get ready to DROP the ball, then press Enter…")
            print(f"  Sampling for 0.5 s — DROP THE BALL NOW!")
            peak, impact_readings = burst_peak(bus, addr, duration_s=0.5)
            all_peaks.append(peak)

            is_dict = stats(impact_readings)
            print(f"  Impact burst: {fmt_stats(is_dict)}")
            print(f"  *** Peak voltage this round: {peak:+.4f} V ***")
            print()

    # ── Summary ──────────────────────────────────────────────────────────────
    print("=" * 55)
    print("CALIBRATION SUMMARY")
    print("=" * 55)

    total_baseline_stats = stats(all_baseline)
    peaks_stats          = stats(all_peaks)

    print(f"Baseline (all rounds) : {fmt_stats(total_baseline_stats)}")
    print(f"Peak per round        : {fmt_stats(peaks_stats)}")
    print()

    # Recommend threshold as midpoint between baseline max and minimum peak,
    # with a small safety margin of 10 % of the gap.
    baseline_ceiling = total_baseline_stats["mean"] + 2 * total_baseline_stats["stdev"]
    min_peak         = peaks_stats["min"]
    gap              = min_peak - baseline_ceiling

    if gap <= 0:
        print("WARNING: baseline ceiling overlaps with the minimum peak voltage.")
        print("         The sensor signal may not be strong enough for reliable detection.")
        print("         Consider increasing the gain / checking wiring.")
        recommended = (total_baseline_stats["mean"] + min_peak) / 2
    else:
        margin      = 0.10 * gap          # 10 % safety margin
        recommended = baseline_ceiling + margin

    print(f"Baseline ceiling (mean + 2σ) : {baseline_ceiling:+.4f} V")
    print(f"Minimum peak across rounds   : {min_peak:+.4f} V")
    print(f"Gap                          : {gap:+.4f} V")
    print()
    print(f"╔══════════════════════════════════════════╗")
    print(f"║  Recommended PRESSURE_THRESHOLD_VOLTS    ║")
    print(f"║                                          ║")
    print(f"║          {recommended:+.4f} V                    ║")
    print(f"╚══════════════════════════════════════════╝")
    print()
    print("Copy this value to config.yaml:")
    print(f"  PRESSURE_THRESHOLD_VOLTS: {recommended:.4f}")
    print()


# ---------------------------------------------------------------------------
# CLI entry point
# ---------------------------------------------------------------------------

def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description="Calibrate the ADS1115 pressure sensor for ball-impact detection.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    p.add_argument("--rounds",   type=int, default=5,    help="Number of calibration rounds (default: 5)")
    p.add_argument("--samples",  type=int, default=10,   help="Baseline samples per round (default: 10)")
    p.add_argument("--bus",      type=int, default=1,    help="I2C bus number (default: 1 → /dev/i2c-1)")
    p.add_argument("--addr",     type=lambda x: int(x, 0), default=0x48,
                   help="ADS1115 I2C address in hex (default: 0x48)")
    return p.parse_args()


if __name__ == "__main__":
    args = parse_args()
    try:
        calibrate(
            bus_num          = args.bus,
            addr             = args.addr,
            rounds           = args.rounds,
            baseline_samples = args.samples,
        )
    except KeyboardInterrupt:
        print("\nAborted.")
        sys.exit(0)
    except OSError as e:
        print(f"\nI2C error: {e}", file=sys.stderr)
        print("Check that I2C is enabled (raspi-config → Interface Options → I2C).", file=sys.stderr)
        sys.exit(1)
