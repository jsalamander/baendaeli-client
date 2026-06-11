# Copilot Instructions: Device State Machine Reference

This repository uses a device-driven state machine. The UI is display-only and must not orchestrate payment creation, payment polling, or dispense actions.

## Hardware Inventory

- Control computer: Raspberry Pi 5, 8 GB, powered by the official 27 W USB-C supply and housed in a Neo case.
- Ball stock: 2.8 cm transparent plastic baubles.
- Actuator: 1500 N linear actuator, 12 V DC, 100 mm stroke.
- Actuator power: 12 V, 10 A, 120 W supply.
- Vibrator: Mini-Vibrationsmotor, 12 V/24 V, 3800 U/min, 10 W.
- Sensor: Purecrea RGB colour sensor based on the TCS34725 with IR filter and LED.

## Hardware Setup

The physical setup is a funnel feeding balls into a movable wood piece that is connected to the actuator. A Purecrea TCS34725 RGB color sensor sits below the wood piece and reads through a glass-protected surface. In the initial state, the wood piece is positioned so the hole in the wood is aligned over the sensor area, and the funnel outlet sits directly above it.

The ball drops from the funnel into the hole in the wood piece and comes to rest above the RGB sensor. The sensor's job is to confirm that a ball has successfully left the funnel and reached the sensor area without jamming in the funnel. A vibrator is mounted directly on the funnel to help clear jams when the ball does not drop cleanly.

When the ball is detected, the actuator moves the wood piece for a fixed amount of time while the ball remains seated in the hole. At the end of that motion, the hole in the base plate opens and the ball falls out below the system.

## Canonical State Flow

1. `starting`
2. `startup_cycle`
3. `detecting_ball`
4. `ball_detected`
5. `awaiting_payment`
6. `dispensing`
7. back to `detecting_ball`

Current payment API contract note:

- `POST /api/v1/payment` returns the initial QR payload.
- `GET /api/v1/payment/{id}` exposes `id`, `status`, `amount_cents`, and `payment_phase`.
- While `payment_phase` is `waiting_for_amount`, the client stays in `ball_detected` so the QR remains visible.
- When `payment_phase` becomes `waiting_for_payment`, the client transitions to `awaiting_payment` and hides the QR.
- `amount_cents` may be present, but `payment_phase` is the authoritative transition signal.

Error and recovery branches:

- `jam`: entered when ball detection fails after retries (includes vibrator-based recovery attempts).
- `payment_failed`: entered when payment status is failed/cancelled/expired/timeout, then reset to `detecting_ball`.
- `error`: entered when payment create/status or dispense operations fail unexpectedly.
- `command_executing`: transient state while operator command path executes.

## Ownership Rules

- Device state transitions are implemented in `internal/device/client.go`.
- HTTP state exposure for frontend rendering is via `GET /api/device/status` in `internal/server/server.go`.
- Frontend rendering is in `internal/server/templates/main.js` and must only consume `/api/device/status`.

## Invariants

- Startup must run one extractor cycle before normal detect loop.
- Payment creation is triggered by ball detection in device logic.
- Payment status polling is performed by device logic, not the browser.
- Dispense is triggered only after a paid/success payment status.
- After dispense and post-dispense ball readiness check, flow returns to `detecting_ball`.

## Testing Expectations

When changing state-machine behavior, keep tests updated in:

- `internal/device/client_test.go` for state transitions and snapshot fields.
- `internal/server/server_test.go` for `/api/device/status` response shape and display-only `main.js` assertions.

Run:

```bash
go test ./...
```
