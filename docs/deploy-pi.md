# Deploying to Raspberry Pi (systemd + auto-update)

## What this gives you
- Runs baendaeli-client as a systemd service
- Stores config and secrets in `/opt/baendaeli-client/config.yaml`
- Manual update via `/usr/local/sbin/baendaeli-update.sh`

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
- Creates /opt/baendaeli-client directory
- Writes systemd unit baendaeli-client.service (runs the app)
- Creates /usr/local/sbin/baendaeli-update.sh for manual updates
- Enables and starts the service

## Quick start (summary)
1) Run installer (one-shot):
  - `curl -fsSL https://jsalamander.github.io/baendaeli-client/install_pi.sh | sudo bash`
2) Add config with secrets:
   - `sudo cat > /opt/baendaeli-client/config.yaml <<EOF`
   - Add your BAENDAELI_API_KEY, URL, and other settings
   - `EOF`
3) Restrict file permissions:
   - `sudo chmod 600 /opt/baendaeli-client/config.yaml`
4) Check status:
   - `sudo systemctl status baendaeli-client.service`
   - Logs: `journalctl -u baendaeli-client.service -f`
5) Manual update (no timer):
```bash
sudo systemctl status baendaeli-client.service
sudo journalctl -u baendaeli-client.service -f
sudo systemctl restart baendaeli-client.service
```

## Managing updates
- Manual update: `sudo /usr/local/sbin/baendaeli-update.sh`

## Config & secrets

Store all configuration in a single YAML file at `/opt/baendaeli-client/config.yaml`.

The systemd service sets `WorkingDirectory=/opt/baendaeli-client`, so the app can load config.yaml via relative path.

### Setup example

```bash
# Create config file with all settings and secrets
sudo cat > /opt/baendaeli-client/config.yaml <<EOF
BAENDAELI_API_KEY: "your-api-key-here"
BAENDAELI_URL: "https://api.baendaeli.example.com"
DEFAULT_AMOUNT_CENTS: 2000
SUCCESS_OVERLAY_MILLIS: 10000
ACTUATOR_ENABLED: false
EOF

# Secure the file: root ownership, read/write for root only (600)
sudo chown root:root /opt/baendaeli-client/config.yaml
sudo chmod 600 /opt/baendaeli-client/config.yaml

# Verify file is secure
ls -l /opt/baendaeli-client/config.yaml
# Should show: -rw------- 1 root root ...

# Verify service can read config
sudo systemctl start baendaeli-client.service
sudo journalctl -u baendaeli-client.service -n 20
```

**Security note:** The config file contains secrets (API keys). Always ensure it has `root:root 600` permissions so only root can read it. The install and update scripts will automatically check and fix permissions if needed.

## Uninstall
```bash
sudo systemctl disable --now baendaeli-client.service
sudo rm -f /etc/systemd/system/baendaeli-client.service \
           /usr/local/sbin/baendaeli-update.sh \
           /usr/local/bin/baendaeli-client
sudo systemctl daemon-reload
```
