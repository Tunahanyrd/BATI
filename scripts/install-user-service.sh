#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "$SCRIPT_DIR/.." && pwd)"
BIN_DIR="${HOME}/.local/bin"
SERVICE_DIR="${HOME}/.config/systemd/user"
APP_DIR="${HOME}/.local/share/applications"
ICON_DIR="${HOME}/.local/share/icons/hicolor/scalable/apps"
SERVICE_NAME="batid.service"
SERVICE_SOURCE="${ROOT_DIR}/packaging/systemd/user-local/${SERVICE_NAME}"
DESKTOP_FILE="${APP_DIR}/bati.desktop"
ICON_FILE="${ICON_DIR}/bati.svg"

mkdir -p "$BIN_DIR" "$SERVICE_DIR" "$APP_DIR" "$ICON_DIR"

echo "Building BATI binaries..."
(
  cd "$ROOT_DIR"
  go build -trimpath -buildvcs=false -ldflags="-s -w" -o "$BIN_DIR/bati" ./cmd/bati
  go build -trimpath -buildvcs=false -ldflags="-s -w" -o "$BIN_DIR/batid" ./cmd/batid
  go build -trimpath -buildvcs=false -ldflags="-s -w" -o "$BIN_DIR/batictl" ./cmd/batictl
)

echo "Installing user service..."
install -m 0644 "$SERVICE_SOURCE" "$SERVICE_DIR/$SERVICE_NAME"

echo "Installing desktop launcher..."
install -m 0644 "$ROOT_DIR/packaging/icons/bati.svg" "$ICON_FILE"
cat > "$DESKTOP_FILE" <<EOF
[Desktop Entry]
Type=Application
Name=BATI
Comment=Battery Analytics & Timeline Interface
Exec=${BIN_DIR}/bati
TryExec=${BIN_DIR}/bati
Icon=${ICON_FILE}
Terminal=false
Categories=System;Monitor;
Keywords=battery;power;health;telemetry;
StartupNotify=true
StartupWMClass=BATI
EOF
chmod 0644 "$DESKTOP_FILE"
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$APP_DIR" >/dev/null 2>&1 || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -q "${HOME}/.local/share/icons/hicolor" >/dev/null 2>&1 || true
fi

systemctl --user daemon-reload
systemctl --user enable --now "$SERVICE_NAME"

echo "BATI user service installed."
systemctl --user --no-pager status "$SERVICE_NAME"
echo "BATI desktop launcher installed at $DESKTOP_FILE"
