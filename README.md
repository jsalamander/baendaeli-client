# baendaeli-client
The client for Baendae.li

## Installation

### Quick Install (Linux)

Install the latest release using the automated installer:

```bash
curl https://jsalamander.github.io/baendaeli-client/installer.sh | bash
```

The installer will:
- Detect your system architecture (AMD64 or ARM64 for Raspberry Pi 5)
- Download the appropriate binary
- Install it to `/usr/local/bin/baendaeli-client`

### Manual Installation

Download binaries from the [releases page](https://github.com/jsalamander/baendaeli-client/releases) and make them executable:

```bash
chmod +x baendaeli-client-linux-amd64  # or arm64 for Raspberry Pi 5
./baendaeli-client-linux-amd64
```

## Configuration

Copy the example config file and edit it with your API credentials:

```bash
cp config.yaml.example config.yaml
```

Edit `config.yaml` and set your credentials:
- `BAENDAELI_API_KEY`: Your Baendae.li API key
- `BAENDAELI_URL`: The Baendae.li API URL

Optional GPIO actuator settings:
- `ACTUATOR_ENABLED`: Set to `true` to enable the linear actuator (Raspberry Pi GPIO)
- `ACTUATOR_ENA_PIN`: ENA pin for the motor driver
- `ACTUATOR_IN1_PIN`: IN1 pin for direction control
- `ACTUATOR_IN2_PIN`: IN2 pin for direction control
- `ACTUATOR_EXTEND_SECONDS`: Duration for extending the actuator
- `ACTUATOR_RETRACT_SECONDS`: Duration for retracting the actuator
- `ACTUATOR_PAUSE_SECONDS`: Pause duration between extend and retract

## Running

```bash
baendaeli-client
```

The web server will start on `http://localhost:8000`.

If building from source:

```bash
go run .
```

## Development

### Prerequisites

- Go 1.21 or higher
- Linux environment (for GPIO support)

### Building

```bash
go build -o baendaeli-client .
```

## Releases

Binaries are automatically built and published when creating a GitHub release tag:

```bash
git tag v1.0.0
git push origin v1.0.0
```

The release workflow will:
1. Build binaries for Linux AMD64 and ARM64
2. Create a GitHub Release with binaries attached
3. Generate and publish the installer script to GitHub Pages
