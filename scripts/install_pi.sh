#!/usr/bin/env bash
set -euo pipefail

# Installs baendaeli-client as a systemd service on Raspberry Pi
# - Uses binstaller-generated installer.sh from GitHub Pages
# - Installs to /usr/local/bin/baendaeli-client
# - Sets up /opt/baendaeli-client for config
# - Registers systemd service and auto-update timer

OWNER="jsalamander"
REPO="baendaeli-client"
BINARY="baendaeli-client"
INSTALL_BIN="/usr/local/bin/${BINARY}"
WORKDIR="/opt/${REPO}"
ENV_FILE="/etc/${REPO}.env"
SERVICE_NAME="${REPO}.service"
INSTALLER_URL="https://jsalamander.github.io/baendaeli-client/installer.sh"

require_root() {
  if [[ $EUID -ne 0 ]]; then
    echo "This script must be run as root (sudo)." >&2
    exit 1
  fi
  echo "[INFO] Running as root" >&2
}

install_binary() {
  echo "[INFO] Installing via binstaller (${INSTALLER_URL})" >&2
  echo "[INFO] BINSTALLER_INSTALL_DIR=/usr/local/bin" >&2
  BINSTALLER_INSTALL_DIR="/usr/local/bin" \
    curl -fsSL "$INSTALLER_URL" | bash
  echo "[INFO] Binary installation complete" >&2
}

write_service() {
  echo "[INFO] Writing systemd service: ${SERVICE_NAME}" >&2
  cat >/etc/systemd/system/${SERVICE_NAME} <<'EOF'
[Unit]
Description=Baendaeli Client
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/baendaeli-client
ExecStart=/usr/local/bin/baendaeli-client
EnvironmentFile=-/etc/baendaeli-client.env
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
  echo "[INFO] Service file written to /etc/systemd/system/${SERVICE_NAME}" >&2
}

write_update_script() {
  echo "[INFO] Writing update script to /usr/local/sbin/baendaeli-update.sh" >&2
  cat >/usr/local/sbin/baendaeli-update.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
OWNER="jsalamander"
REPO="baendaeli-client"
BINARY="baendaeli-client"
INSTALL_BIN="/usr/local/bin/${BINARY}"
INSTALLER_URL="https://jsalamander.github.io/baendaeli-client/installer.sh"

main() {
  BINSTALLER_INSTALL_DIR="/usr/local/bin" \
    curl -fsSL "$INSTALLER_URL" | bash
  systemctl restart baendaeli-client.service || true
}

main "$@"
EOF
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
  mkdir -p "$WORKDIR"
  echo "[INFO] Creating/touching env file: ${ENV_FILE}" >&2
  touch "$ENV_FILE"
  
  echo "[INFO] Installing binary from: ${INSTALLER_URL}" >&2
  install_binary
  
  write_service
  
  write_update_script
  
  echo "[INFO] Reloading systemd daemon" >&2
  systemctl daemon-reload
  
  echo "[INFO] Enabling service: ${SERVICE_NAME}" >&2
  systemctl enable --now ${SERVICE_NAME}
  
  echo "[SUCCESS] Installation complete!" >&2
  echo "Installed. Edit $ENV_FILE for secrets and place config.yaml in $WORKDIR." >&2
  echo "Manual update: sudo /usr/local/sbin/baendaeli-update.sh" >&2
}

main "$@"
