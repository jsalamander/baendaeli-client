#!/usr/bin/env bash
set -euo pipefail

OWNER="jsalamander"
REPO="baendaeli-client"
BINARY="baendaeli-client"
INSTALL_BIN="/usr/local/bin/${BINARY}"
SERVICE="baendaeli-client.service"

detect_arch() {
  case "$(uname -m)" in
    armv7l|armv6l) echo "linux-armhf";;
    aarch64) echo "linux-arm64";;
    *) echo "linux-arm64";;
  esac
}

latest_url() {
  curl -s "https://api.github.com/repos/${OWNER}/${REPO}/releases/latest" \
    | grep "browser_download_url" \
    | grep "$(detect_arch)" \
    | head -n1 \
    | cut -d '"' -f4
}

main() {
  if [[ $EUID -ne 0 ]]; then
    echo "Run as root (sudo)." >&2
    exit 1
  fi

  url=$(latest_url)
  if [[ -z "$url" ]]; then
    echo "No release asset found for $(detect_arch)" >&2
    exit 1
  fi
  tmp=$(mktemp)
  curl -L "$url" -o "$tmp"
  install -m 0755 "$tmp" "$INSTALL_BIN"
  rm -f "$tmp"
  systemctl restart "$SERVICE" || true
}

main "$@"
