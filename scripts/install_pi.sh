#!/usr/bin/env bash
set -euo pipefail

# Installs baendaeli-client as a systemd service on Raspberry Pi
# - Downloads latest release binary from GitHub
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
UPDATE_SERVICE="${REPO}-update.service"
UPDATE_TIMER="${REPO}-update.timer"

require_root() {
  if [[ $EUID -ne 0 ]]; then
    echo "This script must be run as root (sudo)." >&2
    exit 1
  fi
}

detect_arch() {
  local arch
  arch=$(uname -m)
  case "$arch" in
    armv7l|armv6l) echo "linux-armhf";;
    aarch64) echo "linux-arm64";;
    *) echo "linux-arm64";;
  esac
}

fetch_latest_url() {
  local arch="$1"
  curl -s "https://api.github.com/repos/${OWNER}/${REPO}/releases/latest" \
    | grep "browser_download_url" \
    | grep "${arch}" \
    | head -n1 \
    | cut -d '"' -f4
}

install_binary() {
  local arch url tmp
  arch=$(detect_arch)
  url=$(fetch_latest_url "$arch")
  if [[ -z "$url" ]]; then
    echo "Could not find release asset for arch ${arch}." >&2
    exit 1
  fi
  echo "Downloading ${url}" >&2
  tmp=$(mktemp)
  curl -L "$url" -o "$tmp"
  install -m 0755 "$tmp" "$INSTALL_BIN"
  rm -f "$tmp"
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

write_update_service() {
  cat >/etc/systemd/system/${UPDATE_SERVICE} <<'EOF'
[Unit]
Description=Baendaeli Client updater

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/baendaeli-update.sh
EOF
}

write_update_timer() {
  cat >/etc/systemd/system/${UPDATE_TIMER} <<'EOF'
[Unit]
Description=Daily update for Baendaeli Client

[Timer]
OnCalendar=03:00
Persistent=true

[Install]
WantedBy=timers.target
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

arch() {
  case "$(uname -m)" in
    armv7l|armv6l) echo "linux-armhf";;
    aarch64) echo "linux-arm64";;
    *) echo "linux-arm64";;
  esac
}

latest_url() {
  curl -s "https://api.github.com/repos/${OWNER}/${REPO}/releases/latest" \
    | grep "browser_download_url" \
    | grep "$(arch)" \
    | head -n1 \
    | cut -d '"' -f4
}

main() {
  url=$(latest_url)
  if [[ -z "$url" ]]; then
    echo "No release asset found for $(arch)" >&2
    exit 1
  fi
  tmp=$(mktemp)
  curl -L "$url" -o "$tmp"
  install -m 0755 "$tmp" "$INSTALL_BIN"
  rm -f "$tmp"
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
  write_update_service
  write_update_timer
  write_update_script
  systemctl daemon-reload
  systemctl enable --now ${SERVICE_NAME}
  systemctl enable --now ${UPDATE_TIMER}
  echo "Installed. Edit $ENV_FILE for secrets and place config.yaml in $WORKDIR." >&2
}

main "$@"
