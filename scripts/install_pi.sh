#!/usr/bin/env bash
set -euo pipefail

# Installs baendaeli-client as a systemd service on Raspberry Pi
# - Uses binstaller-generated installer.sh from GitHub Pages
# - Installs to /usr/local/bin/baendaeli-client
# - Sets up /opt/baendaeli-client for config
# - Registers systemd service and auto-update timer

trap 'echo "[ERROR] Installation failed at line $LINENO" >&2; exit 1' ERR

OWNER="jsalamander"
REPO="baendaeli-client"
BINARY="baendaeli-client"
INSTALL_BIN="/usr/local/bin/${BINARY}"
WORKDIR="/opt/${REPO}"
SERVICE_NAME="${REPO}.service"
INSTALLER_URL="https://jsalamander.github.io/baendaeli-client/installer.sh"

require_root() {
  if [[ $EUID -ne 0 ]]; then
    echo "[ERROR] This script must be run as root (sudo)." >&2
    exit 1
  fi
  echo "[INFO] Running as root" >&2
}

install_binary() {
  echo "[INFO] Installing via binstaller (${INSTALLER_URL})" >&2
  echo "[INFO] Installing to: /usr/local/bin" >&2
  if ! curl -fsSL "$INSTALLER_URL" | bash -s -- -b "/usr/local/bin"; then
    echo "[ERROR] Binary installation failed" >&2
    return 1
  fi
  echo "[INFO] Binary installation complete" >&2
}

write_service() {
  echo "[INFO] Writing systemd service: ${SERVICE_NAME}" >&2
  if ! cat >/etc/systemd/system/${SERVICE_NAME} <<'EOF'
[Unit]
Description=Baendaeli Client
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/baendaeli-client
ExecStart=/usr/local/bin/baendaeli-client
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
  then
    echo "[ERROR] Failed to write service file to /etc/systemd/system/${SERVICE_NAME}" >&2
    return 1
  fi
  echo "[INFO] Service file written to /etc/systemd/system/${SERVICE_NAME}" >&2
}

check_config_permissions() {
  local config_file="$WORKDIR/config.yaml"
  if [[ -f "$config_file" ]]; then
    local perms
    perms=$(stat -c "%a" "$config_file" 2>/dev/null || stat -f "%A" "$config_file" 2>/dev/null || echo "unknown")
    echo "[INFO] Checking config file: $config_file" >&2
    echo "[INFO] Current permissions: $perms, owner: $(stat -c "%U:%G" "$config_file" 2>/dev/null || stat -f "%Su:%Sg" "$config_file" 2>/dev/null)" >&2
    
    # Ensure root ownership
    if ! chown root:root "$config_file"; then
      echo "[ERROR] Failed to change config file ownership to root:root" >&2
      return 1
    fi
    
    # Ensure 600 permissions
    if ! chmod 600 "$config_file"; then
      echo "[ERROR] Failed to set config file permissions to 600" >&2
      return 1
    fi
    echo "[INFO] Config file secured: root:root 600" >&2
  fi
}

write_update_script() {
  echo "[INFO] Writing update script to /usr/local/sbin/baendaeli-update.sh" >&2
  if ! cat >/usr/local/sbin/baendaeli-update.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
trap 'echo "[ERROR] Update failed at line $LINENO" >&2; exit 1' ERR
OWNER="jsalamander"
REPO="baendaeli-client"
BINARY="baendaeli-client"
INSTALL_BIN="/usr/local/bin/${BINARY}"
INSTALLER_URL="https://jsalamander.github.io/baendaeli-client/installer.sh"

main() {
  echo "[INFO] Starting update check" >&2
  echo "[INFO] Fetching from: ${INSTALLER_URL}" >&2
  if ! curl -fsSL "$INSTALLER_URL" | bash -s -- -b "/usr/local/bin"; then
    echo "[ERROR] Update failed: installer script execution error" >&2
    return 1
  fi
  echo "[INFO] Update complete, restarting service" >&2
  if ! systemctl restart baendaeli-client.service; then
    echo "[ERROR] Failed to restart service" >&2
    return 1
  fi
  echo "[SUCCESS] Update and restart complete" >&2
}

main "$@"
EOF
  then
    echo "[ERROR] Failed to write update script to /usr/local/sbin/baendaeli-update.sh" >&2
    return 1
  fi
  chmod +x /usr/local/sbin/baendaeli-update.sh
  echo "[INFO] Update script written and made executable" >&2
}

main() {
  echo "[INFO] Starting baendaeli-client installation" >&2
  echo "[INFO] Owner: ${OWNER}, Repo: ${REPO}" >&2
  echo "[INFO] Install directory: ${INSTALL_BIN}" >&2
  echo "[INFO] Work directory: ${WORKDIR}" >&2
  echo "[INFO] Config directory: ${WORKDIR}" >&2
  
  require_root
  
  echo "[INFO] Creating work directory: ${WORKDIR}" >&2
  if ! mkdir -p "$WORKDIR"; then
    echo "[ERROR] Failed to create work directory: ${WORKDIR}" >&2
    return 1
  fi
  
  echo "[INFO] Installing binary from: ${INSTALLER_URL}" >&2
  if ! install_binary; then
    echo "[ERROR] Binary installation failed, aborting" >&2
    return 1
  fi
  
  if ! write_service; then
    echo "[ERROR] Service setup failed, aborting" >&2
    return 1
  fi
  
  echo "[INFO] Checking config file permissions" >&2
  if ! check_config_permissions; then
    echo "[ERROR] Config file permission check failed" >&2
    return 1
  fi
  
  if ! write_update_script; then
    echo "[ERROR] Update script setup failed, aborting" >&2
    return 1
  fi
  
  echo "[INFO] Reloading systemd daemon" >&2
  if ! systemctl daemon-reload; then
    echo "[ERROR] Failed to reload systemd daemon" >&2
    return 1
  fi
  
  echo "[INFO] Enabling service: ${SERVICE_NAME}" >&2
  if ! systemctl enable --now ${SERVICE_NAME}; then
    echo "[ERROR] Failed to enable/start service" >&2
    return 1
  fi
  
  echo "[SUCCESS] Installation complete!" >&2
  echo "Installed. Place config.yaml in $WORKDIR." >&2
  echo "Manual update: sudo /usr/local/sbin/baendaeli-update.sh" >&2
}

main "$@"
