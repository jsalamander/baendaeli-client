# Device API Client - Quick Reference

## Implementation Overview

The baendaeli-client now implements the Device API protocol as a polling client that:
- Reports device status with payment ID every 7 seconds
- Polls for commands from the server
- Executes commands (extend/retract/home) via the actuator
- Acknowledges command completion

## How It Works

### Startup
```bash
./baendaeli-client
```

The application automatically:
1. Loads configuration from `config.yaml`
2. Initializes the HTTP server (port 8000)
3. Starts the device client polling loop
4. Begins sending status reports to the API server

### Payment Flow

```
Browser creates payment
    ↓
Server calls /api/payment (forwards to device API)
    ↓
Server receives payment ID
    ↓
Server updates device client with SetPaymentID()
    ↓
Device client includes payment_id in next status report
    ↓
Server knows this device has active payment
```

### Command Flow

```
Device API server queues command for this device
    ↓
Device client polls GET /api/v1/device/commands
    ↓
Device client receives {id: 42, command: "extend"}
    ↓
Device client executes actuator.Extend()
    ↓
Device client sends POST /api/v1/device/commands/42/ack
    ↓
Command is marked as completed
```

## Configuration

No new configuration required! The device client uses:

| Setting | Value | Source |
|---------|-------|--------|
| API Key | BaendaeliAPIKey | config.yaml |
| API URL | BaendaeliURL | config.yaml |
| Movement Duration | ActuatorMovement | config.yaml |
| Poll Interval | 7 seconds | hardcoded (reliable) |

Example config.yaml:
```yaml
BAENDAELI_API_KEY: "device_token_xyz"
BAENDAELI_URL: "https://api.example.com"
ACTUATOR_MOVEMENT_SECONDS: 2
```

## API Endpoints

### 1. Report Status
**POST** `/api/v1/device/status`
- Sent every 7 seconds
- Includes current payment_id
- Returns: `{success: true}`

### 2. Get Command
**GET** `/api/v1/device/commands`
- Fetches next pending command
- Returns: `{id: 42, command: "extend"}` or `{command: null}`

### 3. Acknowledge Command
**POST** `/api/v1/device/commands/{id}/ack`
- Sent after command execution
- Returns: `{success: true}`

## Logging

The device client logs:
```
Device client started
Device client: executing command 42: extend
Device client: acknowledged command 42
Device client: failed to report status: ...
Device client stopped
```

## Troubleshooting

### Device not sending status
- Check `BAENDAELI_URL` is correct
- Check `BAENDAELI_API_KEY` is valid
- Check server logs for 401 errors

### Commands not executing
- Check `ACTUATOR_ENABLED: true` in config
- Check GPIO pins are configured correctly
- Check logs for actuator errors

### Client stops polling
- Check for panic messages in logs
- Device client always continues on error
- Graceful shutdown only on SIGTERM/SIGINT

## Testing

Run device client tests:
```bash
go test -v ./internal/device
```

All tests should pass:
- ✓ Status reporting (4 scenarios)
- ✓ Command fetching (3 scenarios)
- ✓ Command acknowledgment (3 scenarios)
- ✓ Lifecycle management
- ✓ URL building
- ✓ Payment ID tracking

## Files

**Core Implementation:**
- `internal/device/client.go` - Device polling client (305 lines)
- `internal/device/client_test.go` - Comprehensive tests (336 lines)

**Integration:**
- `main.go` - Device client startup/shutdown
- `internal/server/server.go` - Payment ID sync

**Documentation:**
- `docs/device-api-client.md` - Technical reference
- `DEVICE_CLIENT_IMPLEMENTATION.md` - Implementation details

## Performance

- **Polling interval**: 7 seconds
- **HTTP timeout**: 15 seconds
- **Memory overhead**: ~2MB
- **CPU usage**: Minimal (idle between polls)

## Security

- Authenticates with Bearer token
- Uses HTTPS (via config.BaendaeliURL)
- No credentials in logs
- Thread-safe payment ID handling

## Support

For issues or questions:
1. Check logs: `Device client: ...`
2. Review config.yaml settings
3. Verify API connectivity
4. Run tests: `go test -v ./internal/device`
