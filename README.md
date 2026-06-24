# BATI

BATI is a Linux battery analytics app built with Go and Gio. It keeps telemetry local, records history with a small user daemon, and shows current battery state without confusing live sysfs values with stale SQLite history.

## Components

- `bati`: desktop GUI.
- `batid`: user-level telemetry daemon.
- `batictl`: diagnostics and export utility.

## Install From Source

```bash
./scripts/install-user-service.sh
```

The script builds the three binaries into `~/.local/bin`, installs a user-level `batid.service`, and adds a desktop launcher.

```bash
batictl status
systemctl --user status batid.service
```

To remove the user service and desktop launcher without deleting the database:

```bash
./scripts/uninstall-user-service.sh
```

## Release Packages

The v0.1.0 release pipeline builds Linux `amd64` archives and distro packages:

- Debian/Ubuntu: `sudo apt install ./bati_0.1.0_linux_amd64.deb`
- Fedora/openSUSE/RHEL-family: `sudo dnf install ./bati_0.1.0_linux_amd64.rpm`
- Alpine: `sudo apk add --allow-untrusted ./bati_0.1.0_linux_amd64.apk`
- Generic Linux: unpack `bati_0.1.0_linux_amd64.tar.gz`

After installing a distro package, start the per-user daemon:

```bash
systemctl --user enable --now batid.service
batictl status
```

The GUI depends on the normal Linux Wayland/X11/EGL/Vulkan loader libraries used by Gio applications. Package metadata declares the common runtime dependencies for `.deb`, `.rpm`, and `.apk` builds.

## Data

BATI reads battery data from `/sys/class/power_supply`, optional UPower signals, and logind sleep/resume events. The SQLite database is stored at:

```text
~/.local/share/bati/bati.db
```

No telemetry leaves the machine.
