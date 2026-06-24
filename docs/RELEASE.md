# Release Process

BATI releases are built by GitHub Actions with GoReleaser.

## CI

Every push to `main` and every pull request runs:

- `gofmt` check
- `go mod tidy` drift check
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- command builds for `bati`, `batid`, and `batictl`
- install script syntax validation
- desktop entry validation
- GoReleaser snapshot packaging

## Publishing

Create a semver tag and push it:

```bash
git tag -a v0.1.0 -m "BATI v0.1.0"
git push origin v0.1.0
```

The `Release` workflow publishes a GitHub release with:

- `bati_<version>_linux_amd64.tar.gz`
- `bati_<version>_linux_amd64.deb`
- `bati_<version>_linux_amd64.rpm`
- `bati_<version>_linux_amd64.apk`
- `checksums.txt`

## Package Layout

System packages install:

- `/usr/bin/bati`
- `/usr/bin/batid`
- `/usr/bin/batictl`
- `/usr/share/applications/bati.desktop`
- `/usr/share/icons/hicolor/scalable/apps/bati.svg`
- `/usr/lib/systemd/user/batid.service`

Users start the daemon after package install:

```bash
systemctl --user enable --now batid.service
batictl status
```

The source installer remains separate and targets `~/.local/bin` plus `~/.config/systemd/user`.
