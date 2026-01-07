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
}

install_binary() {
  echo "Installing via binstaller (${INSTALLER_URL})" >&2
  BINSTALLER_INSTALL_DIR="/usr/local/bin" \
    curl -fsSL "$INSTALLER_URL" | bash
}

write_service() {
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
}

write_update_script() {
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
}

main() {
  require_root
  mkdir -p "$WORKDIR"
  touch "$ENV_FILE"
  install_binary
  write_service
  write_update_script
  systemctl daemon-reload
  systemctl enable --now ${SERVICE_NAME}
  echo "Installed. Edit $ENV_FILE for secrets and place config.yaml in $WORKDIR." >&2
  echo "Manual update: sudo /usr/local/sbin/baendaeli-update.sh" >&2
}

main "$@"
