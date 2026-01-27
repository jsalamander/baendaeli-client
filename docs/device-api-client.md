# Device API Client Implementation

## Overview

The device client implements the Device API polling protocol, enabling the baendaeli-client to act as a device that:

1. **Reports status** to the server with current payment ID
2. **Polls for commands** (extend, retract, home)
3. **Executes commands** via the actuator
4. **Acknowledges** completed commands

## Architecture

### Core Components

**`internal/device/client.go`** - Main polling client
- Manages HTTP communication with the device API server
- Runs a polling loop every 7 seconds
- Tracks current payment ID
- Thread-safe using mutex protection

**`internal/server/server.go` (modified)** - Server integration
- Holds reference to device client
- Updates payment ID when new payments are created
- Passes payment ID to device client via `SetPaymentID()`

**`main.go` (modified)** - Startup integration
- Creates device client instance
- Starts client as background goroutine
- Gracefully stops client on shutdown

## Polling Loop

The device client runs the following loop every 7 seconds:

```go
1. POST /api/v1/device/status
   - Sends current payment_id to server
   - No client_info (ignored per requirements)

2. GET /api/v1/device/commands
   - Fetches next pending command
   - Returns command ID and type (extend, retract, home)

3. If command received:
   - Execute via actuator (maps to Extend, Retract, or Home)
   - POST /api/v1/device/commands/{id}/ack
   - Acknowledge command completion
```

## API Integration

### Authentication
All requests include the Bearer token from `config.BaendaeliAPIKey`:
```
Authorization: Bearer <API_KEY>
```

### Status Report
**POST** `/api/v1/device/status`
```json
{
  "payment_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

### Get Command
**GET** `/api/v1/device/commands`
```json
{
  "id": 42,
  "command": "extend",
  "duration_ms": 30000
}
```

**Note:** The `duration_ms` field is optional. If provided and greater than 0, the device will use that duration for the command. If not provided or set to 0, the device will fall back to the configured `ACTUATOR_MOVEMENT_SECONDS` default.

**Supported Commands:**
- `extend`: Extends the actuator
- `retract`: Retracts the actuator
- `home`: Homes the actuator (full retraction)
- `message`: Displays a message on the device UI

**Message Command Example:**
```json
{
  "id": 44,
  "command": "message",
  "message": "Hello Device!",
  "duration_ms": 5000
}
```

The `message` command displays the specified text as a popup overlay on the device UI for the duration specified by `duration_ms`. This is useful for displaying notifications, status updates, or instructions to users at the device.

### Acknowledge Command
**POST** `/api/v1/device/commands/{id}/ack`

## Payment ID Flow

1. Web UI creates payment via `/api/payment`
2. Server receives response with `id` field
3. Server calls `deviceClient.SetPaymentID(id)`
4. Device client includes ID in next status report
5. Server can track which device has active payment

## Error Handling

- **401 Unauthorized**: Invalid or missing API key - logs error, continues polling
- **404 Not Found**: Command not found - logs error, continues
- **Network errors**: Automatically retried on next poll cycle
- **Command execution errors**: Logged but acknowledged to prevent command loop

## Configuration

Uses existing configuration fields:
- `BAENDAELI_URL`: API server URL
- `BAENDAELI_API_KEY`: Device authentication token
- `ACTUATOR_MOVEMENT_SECONDS`: Duration for extend/retract commands

## Testing

Unit tests cover:
- Status reporting
- Command fetching
- Command acknowledgment
- Payment ID management
- URL building
- Client start/stop lifecycle
- Full polling cycle

Run tests with:
```bash
go test -v ./internal/device
```

## Graceful Shutdown

The client implements graceful shutdown:
1. `Stop()` cancels the context
2. Polling loop exits immediately
3. WaitGroup ensures goroutine cleanup
4. All in-flight requests timeout within 15 seconds

## Example Flow

```
User creates payment:
  Browser → Server:     POST /api/payment
  Browser ← Server:     {id: "uuid-123", ...}
  Server → deviceClient: SetPaymentID("uuid-123")

Device polling cycle:
  deviceClient → Server: POST /api/v1/device/status (payment_id: "uuid-123")
  Server → deviceClient: Received status
  
  deviceClient → Server: GET /api/v1/device/commands
  Server → deviceClient: {id: 42, command: "extend"}
  
  deviceClient:          Execute actuator.Extend()
  
  deviceClient → Server: POST /api/v1/device/commands/42/ack
  Server → deviceClient: Acknowledged
```
