#!/usr/bin/env bash
set -euo pipefail

trap 'echo "[ERROR] Update failed at line $LINENO" >&2; exit 1' ERR

OWNER="jsalamander"
REPO="baendaeli-client"
BINARY="baendaeli-client"
INSTALL_BIN="/usr/local/bin/${BINARY}"
SERVICE="baendaeli-client.service"
INSTALLER_URL="https://jsalamander.github.io/baendaeli-client/installer.sh"

main() {
  echo "[INFO] Starting baendaeli-client update" >&2
  echo "[INFO] Owner: ${OWNER}, Repo: ${REPO}" >&2
  echo "[INFO] Service: ${SERVICE}" >&2
  
  if [[ $EUID -ne 0 ]]; then
    echo "[ERROR] This script must be run as root (sudo)." >&2
    exit 1
  fi
  echo "[INFO] Running as root" >&2
  
  echo "[INFO] Fetching latest installer from: ${INSTALLER_URL}" >&2
  if ! curl -fsSL "$INSTALLER_URL" | bash -s -- -b "/usr/local/bin"; then
    echo "[ERROR] Failed to download/install binary" >&2
    return 1
  fi
  echo "[INFO] Binary update complete" >&2
  
  echo "[INFO] Restarting service: ${SERVICE}" >&2
  if ! systemctl restart "$SERVICE"; then
    echo "[ERROR] Failed to restart service" >&2
    return 1
  fi
  echo "[SUCCESS] Update and restart complete" >&2
}

main "$@"
