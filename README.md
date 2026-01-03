# baendaeli-client
The client for Baendae.li

## Configuration

Copy the example config file and edit it with your API credentials:

```bash
cp config.yaml.example config.yaml
```

Edit `config.yaml` and set your credentials:
- `BAENDAELI_API_KEY`: Your Baendae.li API key
- `BAENDAELI_URL`: The Baendae.li API URL

## Running

Build and run the application:

```bash
go build -o baendaeli-client main.go
./baendaeli-client
```

The web server will start on `http://localhost:8000`.

## Development

### Prerequisites

- Go 1.24 or higher

### Building

```bash
go build -o baendaeli-client main.go
```

## Releases

Binaries are automatically built and released when pushing to the `main` branch or creating a tag using GoReleaser.
