# Release Guide

How to ship a new version of AgentBoard.

---

## Cutting a release

From an up-to-date, green `main`:

```bash
git checkout main && git pull origin main
make check
git tag -a v0.2.0 -m "Release v0.2.0"
git push origin v0.2.0
```

That is all. Pushing a `v*` tag runs `.github/workflows/release.yml`, which:

1. Runs `make check` (vet + unit tests).
2. Runs `make dist VERSION=<tag>`: cross-compiles the five binaries below
   with `CGO_ENABLED=0` and the tag stamped into `internal/version.Version`,
   then writes `dist/checksums.txt` (`sha256sum` format).
3. Checks the linux binary's `--version` prints the tag.
4. Publishes a GitHub release with generated notes and every file in
   `dist/` attached. A tag with a `-` (e.g. `v0.3.0-rc1`) is published as a
   prerelease and does not become "latest".

Watch it with `gh run watch` or at
https://github.com/ayandexyz/agent-board/actions.

`make dist VERSION=v0.2.0` builds the same artifacts locally.

---

## How users get it

```bash
curl -fsSL https://agentboard.ayande.xyz/install.sh | bash
```

`install.sh` in this repo is the source of truth. The marketing site
(`agentboard-web`) serves a copy at `public/install.sh` and
`public/uninstall.sh`; after changing either script here, copy it there and
deploy the site.

The installer follows `github.com/ayandexyz/agent-board/releases/latest` to
find the tag (no API call, so no rate limit), downloads the platform binary
and `checksums.txt`, verifies the SHA-256, and installs to `~/.local/bin`.

`agentboard update` does the same from inside the binary: it asks the
GitHub API for the latest release, refuses to move to an older or equal
version, verifies the download against `checksums.txt`, and atomically
renames it over the running executable (following a symlink to its target).
A release without `checksums.txt` cannot be installed by either path.

---

## Platform binaries

| Asset | OS | Arch |
|-------|----|------|
| `agentboard-linux-x86_64` | Linux | x86_64 |
| `agentboard-linux-arm64` | Linux | ARM64 |
| `agentboard-darwin-x86_64` | macOS | Intel |
| `agentboard-darwin-arm64` | macOS | Apple Silicon |
| `agentboard-android-arm64` | Android (Termux) | ARM64 |
| `checksums.txt` | — | SHA-256 of each binary |

These names are a contract between `release.yml`/`Makefile`, `install.sh`,
`npm/bin/download.js` and `selfupdate.AssetName`. Change them together.

---

## Troubleshooting

**Installer says "no published release found".** The tag's workflow failed
or has not finished; check the Actions tab.

**`agentboard update` says the directory is not writable.** The binary was
installed somewhere root owns (e.g. `/usr/local/bin`). Re-run with `sudo`,
or reinstall to `~/.local/bin` with the one-liner.
