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
curl -fsSL https://<your-github-pages-domain>/install_pi.sh | sudo bash

# or fetch the raw script from GitHub
curl -fsSL https://raw.githubusercontent.com/jsalamander/baendaeli-client/main/scripts/install_pi.sh | sudo bash
```
(Or scp scripts/install_pi.sh to the Pi and run sudo ./install_pi.sh)

## What install does
- Detects arch (armhf/arm64) and downloads the latest GitHub release asset
- Installs binary to /usr/local/bin/baendaeli-client
- Creates /opt/baendaeli-client and touches /etc/baendaeli-client.env
- Writes systemd units:
  - baendaeli-client.service (runs the app)
  - baendaeli-client-update.service (runs updater)
  - baendaeli-client-update.timer (daily at 03:00)
- Enables and starts both the service and the timer

## Quick start (summary)
1) Run installer (one-shot):
  - `curl -fsSL https://raw.githubusercontent.com/jsalamander/baendaeli-client/main/scripts/install_pi.sh | sudo bash`
2) Add config & secrets:
  - Copy your config.yaml to /opt/baendaeli-client/config.yaml
  - Add secrets to /etc/baendaeli-client.env (KEY=VALUE lines), e.g. `BAENDAELI_API_KEY=...`
3) Check status:
  - `sudo systemctl status baendaeli-client.service`
  - Logs: `journalctl -u baendaeli-client.service -f`
4) Manual update:
  - `sudo /usr/local/sbin/baendaeli-update.sh`

## Managing the service
```bash
sudo systemctl status baendaeli-client.service
sudo journalctl -u baendaeli-client.service -f
sudo systemctl restart baendaeli-client.service
```

## Managing updates
- Manual update: `sudo /usr/local/sbin/baendaeli-update.sh`
- Timer: `sudo systemctl list-timers baendaeli-client-update.timer`

## Config & secrets
- Place config.yaml at /opt/baendaeli-client/config.yaml
- Put secrets (BAENDAELI_API_KEY, etc.) in /etc/baendaeli-client.env (KEY=VALUE lines)
- Service runs as root by default (needed for GPIO). If you switch user, ensure GPIO access.

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
