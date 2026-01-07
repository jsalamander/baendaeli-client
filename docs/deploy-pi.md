# Deploying to Raspberry Pi (systemd + kiosk)

## What this gives you
- Runs baendaeli-client as a systemd service (auto-boots)
- Stores config and secrets in `/opt/baendaeli-client/config.yaml`
- Optional Chromium kiosk browser auto-starts displaying the interface on `localhost:8000`
- Manual update via `/usr/local/sbin/baendaeli-update.sh`

## Prereqs
- Raspberry Pi with network access
- sudo/root access
- A published GitHub release asset named with arch suffix (linux-armhf or linux-arm64)
- Chromium browser (optional, for kiosk mode): `sudo apt-get install -y chromium-browser` or `chromium`

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
- Creates a dedicated system user `baendaeli-client` (added to `gpio` group for actuator access)
- Creates /opt/baendaeli-client directory (owned by service user)
- Writes two systemd units:
  - `baendaeli-client.service` (runs the backend server as dedicated user, auto-boots)
  - `baendaeli-client-kiosk.service` (runs Chromium in kiosk mode as desktop user)
- Creates /usr/local/sbin/baendaeli-update.sh for manual updates
- Enables and starts both services

## Quick start (summary)
1) Run installer (one-shot):
   - `curl -fsSL https://jsalamander.github.io/baendaeli-client/install_pi.sh | sudo bash`
2) Install Chromium (if not already installed):
   - `sudo apt-get update && sudo apt-get install -y chromium-browser`
3) Add config with secrets:
   - `sudo cat > /opt/baendaeli-client/config.yaml <<EOF`
   - Add your BAENDAELI_API_KEY, URL, and other settings
   - `EOF`
4) Restrict file permissions:
   - `sudo chown baendaeli-client:baendaeli-client /opt/baendaeli-client/config.yaml`
   - `sudo chmod 600 /opt/baendaeli-client/config.yaml`
5) Check status:
   - `sudo systemctl status baendaeli-client.service`
   - `sudo systemctl status baendaeli-client-kiosk.service`
   - Logs: `journalctl -u baendaeli-client.service -f`

## Managing the services
```bash
# Client service (backend)
sudo systemctl status baendaeli-client.service
sudo journalctl -u baendaeli-client.service -f

# Kiosk service (browser)
sudo systemctl status baendaeli-client-kiosk.service
sudo journalctl -u baendaeli-client-kiosk.service -f

# Control services
sudo systemctl restart baendaeli-client.service
sudo systemctl disable baendaeli-client-kiosk.service  # disable kiosk on boot
sudo systemctl start baendaeli-client-kiosk.service    # start kiosk manually
```

## Kiosk browser

The kiosk service automatically starts Chromium in fullscreen kiosk mode when the system boots, displaying the interface at `http://localhost:8000`.

**Requirements:**
- Raspberry Pi OS Desktop (with GUI)
- Auto-login enabled (Settings → Raspberry Pi Configuration → System → Auto login)
- Chromium browser installed

**Service dependency:** The kiosk service waits for the client service to start, then delays 2 seconds before launching the browser.

**How it works:** The installer detects the user running the X session and configures the kiosk service to run as that user (not root), allowing it to access the desktop display.

**Disable kiosk on startup** (but keep the service installed):
```bash
sudo systemctl disable baendaeli-client-kiosk.service
```

**Start kiosk manually later:**
```bash
sudo systemctl start baendaeli-client-kiosk.service
```

**If Chromium is missing:**
```bash
sudo apt-get update && sudo apt-get install -y chromium-browser
# Or on newer systems:
sudo apt-get install -y chromium
```

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

# Secure the file: service user ownership, read/write for service user only (600)
sudo chown baendaeli-client:baendaeli-client /opt/baendaeli-client/config.yaml
sudo chmod 600 /opt/baendaeli-client/config.yaml

# Verify file is secure
ls -l /opt/baendaeli-client/config.yaml
# Should show: -rw------- 1 baendaeli-client baendaeli-client ...

# Verify service can read config
sudo systemctl start baendaeli-client.service
sudo journalctl -u baendaeli-client.service -n 20
```

**Security note:** The config file contains secrets (API keys). Always ensure it has `baendaeli-client:baendaeli-client 600` permissions so only the service user can read it. The install and update scripts will automatically check and fix permissions if needed.

## Manual updates
```bash
sudo /usr/local/sbin/baendaeli-update.sh
```

## Troubleshooting

### Kiosk service not starting
**Check logs:**
```bash
sudo journalctl -u baendaeli-client-kiosk.service -n 50
```

**Common issues:**

1. **Missing X server / Display error**
   - Ensure you have Raspberry Pi OS Desktop (not Lite)
   - Enable auto-login: `sudo raspi-config` → System Options → Boot / Auto Login → Desktop Autologin
   - Reboot after enabling auto-login

2. **Chromium not found (exit code 203)**
   - Install: `sudo apt-get install -y chromium-browser` or `sudo apt-get install -y chromium`
   - Verify: `which chromium` or `which chromium-browser`

3. **Service crashes immediately**
   - Check if client service is running: `sudo systemctl status baendaeli-client.service`
   - Verify port 8000 is listening: `sudo netstat -tlnp | grep 8000`
   - Check client logs: `sudo journalctl -u baendaeli-client.service -n 50`

### Client service not starting
**Check logs:**
```bash
sudo journalctl -u baendaeli-client.service -n 50
```

**Common issues:**

1. **Config file not found**
   - Create config: See "Config & secrets" section above
   - Verify location: `ls -la /opt/baendaeli-client/config.yaml`

2. **Permission denied**
   - Fix ownership: `sudo chown baendaeli-client:baendaeli-client /opt/baendaeli-client/config.yaml`
   - Fix permissions: `sudo chmod 600 /opt/baendaeli-client/config.yaml`

3. **Port 8000 already in use**
   - Check what's using it: `sudo netstat -tlnp | grep 8000`
   - Kill the process or change port in config

4. **GPIO/Actuator errors**
   - Verify user in gpio group: `groups baendaeli-client`
   - If missing: `sudo usermod -a -G gpio baendaeli-client && sudo systemctl restart baendaeli-client.service`

### No internet connectivity shown
- The web interface checks internet connectivity every 10 seconds
- Uses Cloudflare CDN endpoint for reliable checking
- Red indicator = offline, Green = online
- If stuck on yellow: Check browser console for errors

### Service user issues
The service runs as the dedicated `baendaeli-client` system user (not root) for security.

**View service user info:**
```bash
id baendaeli-client
groups baendaeli-client
```

**Recreate service user if needed:**
```bash
sudo userdel baendaeli-client
# Re-run installer to recreate
```

## Uninstall
```bash
# Stop and disable services
sudo systemctl disable --now baendaeli-client.service baendaeli-client-kiosk.service

# Remove service files
sudo rm -f /etc/systemd/system/baendaeli-client.service \
           /etc/systemd/system/baendaeli-client-kiosk.service \
           /usr/local/sbin/baendaeli-update.sh \
           /usr/local/bin/baendaeli-client

# Remove service user (optional)
sudo userdel baendaeli-client

# Remove config and data (optional - contains your secrets!)
sudo rm -rf /opt/baendaeli-client

# Reload systemd
sudo systemctl daemon-reload
```
