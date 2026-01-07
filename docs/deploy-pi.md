# Deploying to Raspberry Pi (systemd + auto-update)

## What this gives you
- Runs baendaeli-client as a systemd service
- Keeps config in /opt/baendaeli-client (config.yaml) and secrets in /etc/baendaeli-client.env
- Daily auto-update via systemd timer calling a small updater script

## Prereqs
- Raspberry Pi with network access
- sudo/root access
- A published GitHub release asset named with arch suffix (linux-armhf or linux-arm64)

## One-shot install
```bash
curl -fsSL https://jsalamander.github.io/baendaeli-client/install_pi.sh | sudo bash

# or fetch the raw script from GitHub
curl -fsSL https://raw.githubusercontent.com/jsalamander/baendaeli-client/main/scripts/install_pi.sh | sudo bash
```
(Or scp scripts/install_pi.sh to the Pi and run sudo ./install_pi.sh)

## What install does
- Detects arch (armhf/arm64) and downloads the latest GitHub release asset via the binstaller installer
- Installs binary to /usr/local/bin/baendaeli-client
- Creates /opt/baendaeli-client and touches /etc/baendaeli-client.env
- Writes systemd unit baendaeli-client.service (runs the app)
- Enables and starts the service

## Quick start (summary)
1) Run installer (one-shot):
  - `curl -fsSL https://jsalamander.github.io/baendaeli-client/install_pi.sh | sudo bash`
2) Add config & secrets:
  - Copy your config.yaml to /opt/baendaeli-client/config.yaml
  - Add secrets to /etc/baendaeli-client.env (KEY=VALUE lines), e.g. `BAENDAELI_API_KEY=...`
3) Check status:
  - `sudo systemctl status baendaeli-client.service`
  - Logs: `journalctl -u baendaeli-client.service -f`
4) Manual update (no timer):
  - `sudo /usr/local/sbin/baendaeli-update.sh`

## Managing the service
```bash
sudo systemctl status baendaeli-client.service
sudo journalctl -u baendaeli-client.service -f
sudo systemctl restart baendaeli-client.service
```

## Managing updates
- Manual update: `sudo /usr/local/sbin/baendaeli-update.sh`

## Config & secrets

The systemd service locates files as follows:

- **Config file**: `/opt/baendaeli-client/config.yaml`
  - Service sets `WorkingDirectory=/opt/baendaeli-client`
  - App calls `config.Load("config.yaml")` (relative path)
  - **You must place your config.yaml here**

- **Environment file**: `/etc/baendaeli-client.env` (optional)
  - Loaded by service's `EnvironmentFile=-/etc/baendaeli-client.env` directive
  - Use for secrets or overrides: `KEY=VALUE` format, one per line
  - Example: `BAENDAELI_API_KEY=your-api-key-here`
  - The `-` prefix means "ignore if missing"
  - Currently, the app reads config from YAML; env vars can be used for future enhancements or shell integration

### Setup example

```bash
# 1. Create config file
sudo cat > /opt/baendaeli-client/config.yaml <<EOF
BAENDAELI_API_KEY: "your-api-key-here"
BAENDAELI_URL: "https://api.baendaeli.example.com"
DEFAULT_AMOUNT_CENTS: 2000
SUCCESS_OVERLAY_MILLIS: 10000
ACTUATOR_ENABLED: false
EOF

# 2. (Optional) Create env file for additional secrets
sudo bash -c 'echo "# Environment overrides" > /etc/baendaeli-client.env'

# 3. Verify service can read config
sudo systemctl start baendaeli-client.service
sudo journalctl -u baendaeli-client.service -n 20
```

## Uninstall
```bash
sudo systemctl disable --now baendaeli-client.service baendaeli-client-update.timer
sudo rm -f /etc/systemd/system/baendaeli-client.service \
           /etc/systemd/system/baendaeli-client-update.service \
           /etc/systemd/system/baendaeli-client-update.timer \
           /usr/local/sbin/baendaeli-update.sh \
           /usr/local/bin/baendaeli-client
sudo systemctl daemon-reload
```
