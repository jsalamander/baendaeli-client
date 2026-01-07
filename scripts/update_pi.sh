#!/usr/bin/env bash
set -euo pipefail

OWNER="jsalamander"
REPO="baendaeli-client"
BINARY="baendaeli-client"
INSTALL_BIN="/usr/local/bin/${BINARY}"
SERVICE="baendaeli-client.service"
INSTALLER_URL="https://jsalamander.github.io/baendaeli-client/installer.sh"

main() {
  if [[ $EUID -ne 0 ]]; then
    echo "Run as root (sudo)." >&2
    exit 1
  fi
  BINSTALLER_INSTALL_DIR="/usr/local/bin" \
    curl -fsSL "$INSTALLER_URL" | bash
  systemctl restart "$SERVICE" || true
}

main "$@"
