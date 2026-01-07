#!/usr/bin/env bash
set -euo pipefail

trap 'echo "[ERROR] Update failed at line $LINENO" >&2; exit 1' ERR

OWNER="jsalamander"
REPO="baendaeli-client"
BINARY="baendaeli-client"
INSTALL_BIN="/usr/local/bin/${BINARY}"
SERVICE="baendaeli-client.service"
INSTALLER_URL="https://jsalamander.github.io/baendaeli-client/installer.sh"
WORKDIR="/opt/${REPO}"

check_config_permissions() {
  local config_file="$WORKDIR/config.yaml"
  if [[ -f "$config_file" ]]; then
    local perms
    perms=$(stat -c "%a" "$config_file" 2>/dev/null || stat -f "%A" "$config_file" 2>/dev/null || echo "unknown")
    if [[ "$perms" != "600" ]]; then
      echo "[WARNING] Config file has insecure permissions: $perms (should be 600)" >&2
      echo "[INFO] Fixing permissions..." >&2
      if ! chmod 600 "$config_file"; then
        echo "[ERROR] Failed to fix config file permissions" >&2
        return 1
      fi
      echo "[INFO] Config file permissions fixed to 600" >&2
    else
      echo "[INFO] Config file permissions OK (600)" >&2
    fi
  fi
}

main() {
  echo "[INFO] Starting baendaeli-client update" >&2
  echo "[INFO] Owner: ${OWNER}, Repo: ${REPO}" >&2
  echo "[INFO] Service: ${SERVICE}" >&2
  
  if [[ $EUID -ne 0 ]]; then
    echo "[ERROR] This script must be run as root (sudo)." >&2
    exit 1
  fi
  echo "[INFO] Running as root" >&2
  
  echo "[INFO] Checking config file permissions" >&2
  if ! check_config_permissions; then
    echo "[WARNING] Config file permission check failed, continuing with update" >&2
  fi
  
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
