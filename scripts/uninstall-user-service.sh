#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="batid.service"
SERVICE_PATH="${HOME}/.config/systemd/user/${SERVICE_NAME}"
DESKTOP_FILE="${HOME}/.local/share/applications/bati.desktop"
ICON_FILE="${HOME}/.local/share/icons/hicolor/scalable/apps/bati.svg"

systemctl --user disable --now "$SERVICE_NAME" 2>/dev/null || true
rm -f "$SERVICE_PATH"
rm -f "$DESKTOP_FILE" "$ICON_FILE"
systemctl --user daemon-reload
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "${HOME}/.local/share/applications" >/dev/null 2>&1 || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -q "${HOME}/.local/share/icons/hicolor" >/dev/null 2>&1 || true
fi

echo "BATI user app files and service removed. The local database was left untouched."
