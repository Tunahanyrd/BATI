# Changelog

## v0.1.0 - 2026-06-24

- Added the BATI Gio desktop app, `batid` telemetry daemon, and `batictl` diagnostics CLI.
- Added live sysfs battery refresh for Overview while keeping historical charts backed by SQLite telemetry.
- Added user-level systemd service installation scripts and distro package metadata.
- Added GitHub Actions CI and release packaging for Linux `.deb`, `.rpm`, `.apk`, and `.tar.gz` artifacts.
- Added daemon, diagnostics, database, GUI formatting, tooltip, and chart edge-case tests.
