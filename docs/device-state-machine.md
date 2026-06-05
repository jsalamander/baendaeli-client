# Device State Machine

This diagram reflects the current runtime behavior implemented in `internal/device/client.go`.

```mermaid
stateDiagram-v2
    [*] --> starting: Start()

    starting --> startup_cycle: actuator enabled
    starting --> detecting_ball: actuator disabled

    startup_cycle --> detecting_ball: startup extractor cycle OK
    startup_cycle --> error: startup extractor cycle failed

    detecting_ball --> jam: waitForBallReady failed
    jam --> detecting_ball: passive recovery successful
    jam --> jam: passive recovery failed

    detecting_ball --> ball_detected: ball detected + payment created
    detecting_ball --> error: payment create failed

    ball_detected --> ball_detected: payment waiting/open/pending + phase=waiting_for_amount
    ball_detected --> awaiting_payment: payment waiting/open/pending + phase=waiting_for_payment

    awaiting_payment --> awaiting_payment: payment waiting/open/pending + phase=waiting_for_payment
    awaiting_payment --> ball_detected: payment waiting/open/pending + phase=waiting_for_amount

    ball_detected --> dispensing: payment success/paid/completed
    awaiting_payment --> dispensing: payment success/paid/completed

    ball_detected --> payment_failed: payment failure/failed/cancelled/expired/timeout
    awaiting_payment --> payment_failed: payment failure/failed/cancelled/expired/timeout

    payment_failed --> detecting_ball: payment reset

    dispensing --> detecting_ball: dispense + next-ball check OK
    dispensing --> error: dispense failed

    state "Operator Command Scheduler" as cmd {
        [*] --> command_poll

        command_poll --> pending_command: command fetched but not executable now
        pending_command --> command_poll: next poll

        command_poll --> command_executing: executable command present
        command_executing --> command_poll: ack success/failed

        note right of pending_command
            Deferred while policy blocks execution.
            Examples:
            - load_test, ball_dispenser require clean state
            - home/extend/retract/vibrate blocked in waiting_for_payment
        end note
    }

    note right of command_executing
        Runtime state is set to command_executing
        while command runs and ack is sent.
    end note
```

## Command Policy Summary

- Always executable: `message`, `take_picture`, `cancel`
- Clean-state only: `load_test`, `ball_dispenser`
  - clean state means: no jam, no active payment, state is `detecting_ball` or `idle`
- Actuation commands: `home`, `extend`, `retract`, `vibrate`
  - allowed with active payment except when `payment_phase` is `waiting_for_payment`

## Data Signals Used

- Status endpoint: `GET /api/v1/payment/{id}`
- Transition signal for amount-selection phase split: `payment_phase`
  - `waiting_for_amount` keeps runtime in `ball_detected`
  - `waiting_for_payment` moves runtime to `awaiting_payment`
