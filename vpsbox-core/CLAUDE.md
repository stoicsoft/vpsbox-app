# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`vpsbox` is a single-binary Go CLI (plus an optional Wails desktop wrapper) that boots a local Ubuntu VM and presents it as a fresh "VPS" for trying deploy tools (Server Compass, Coolify, Dokploy, Dokku, CapRover, Kamal, etc.) without actually renting a server. Multipass is the only end-to-end backend; Lima is a stub that returns `ErrUnsupported`.

The CLI writes all state to `~/.vpsbox/` (overridable via `VPSBOX_HOME`) and is designed to be consumed by other tools through the JSON registry at `~/.vpsbox/instances.json` and the `vpsbox export` contract — there is no daemon or RPC.

## Common commands

```bash
# Build the CLI
go build -o bin/vpsbox ./cmd/vpsbox

# Tests (cross-compile checks matter — CI runs the same matrix on macos/ubuntu/windows)
go test ./...
GOOS=linux   GOARCH=amd64 go test ./...
GOOS=windows GOARCH=amd64 go test ./...

# Single package / single test
go test ./internal/registry
go test ./internal/share -run TestManager_Create

# Format check (CI fails if gofmt produces a diff)
gofmt -w $(find . -name '*.go' -type f) && git diff --exit-code

# Cross-compiled release tarballs into dist/
./scripts/release.sh v0.1.0

# Wails desktop app (lives in desktop/ as its own Go module)
cd desktop && go mod tidy && (cd frontend && npm install) && $(go env GOPATH)/bin/wails dev

# Production desktop bundle + macOS .pkg installer (run from repo root)
./scripts/build-desktop.sh
./scripts/package-desktop-macos.sh
```

CI (`.github/workflows/ci.yml`) runs `gofmt` + `go test ./...` on macOS, Ubuntu, and Windows. Keep the cross-platform build green — `internal/doctor` and `internal/share` already split files via `_unix.go` / `_windows.go` build tags, follow that pattern when adding host-specific code.

## Architecture

There are two Go modules in this repo:

1. **Root module** (`github.com/stoicsoft/vpsbox`) — the CLI and all reusable packages under `internal/`.
2. **`desktop/` module** (`github.com/stoicsoft/vpsbox/desktopapp`) — the Wails app. It uses a local `replace` directive (`replace github.com/stoicsoft/vpsbox => ..`) to import the same `internal/app.Manager`, so the desktop UI is a thin shell over the same orchestration layer the CLI uses. Don't fork logic into `desktop/` — extend `internal/app` instead.

### Orchestration layer: `internal/app`

`internal/app.Manager` is the single entry point both the CLI and the desktop app construct via `NewManager(ctx)`. It owns:

- `config.Paths` — every `~/.vpsbox/...` path lives here. Never hardcode a path.
- `registry.Store` — JSON-on-disk persistence for instances, shares, auth.
- `tls.Manager` — `mkcert` integration with self-signed fallback (`--self-signed`).
- `share.Manager` — `cloudflared` quick-tunnel lifecycle.
- `backend.Backend` — VM driver chosen by `backend.Detect`.

`cli.go` wires Cobra commands to `Manager` methods. `ui.go` serves the local HTML dashboard from the same binary on `vpsbox ui`. New top-level commands should be added to both `cli.go` (for the CLI) and exposed as a `Manager` method (so the desktop app can call them).

### Backend abstraction: `internal/backend`

`Backend` is the VM-driver interface (`Create`, `Start`, `Stop`, `Snapshot`, `Restore`, `Info`, `UpdateResources`, `InstallSSHKey`, …). `Detect()` picks one by `Priority()` from `supportedBackends()`, which is OS-gated (Windows only gets Multipass). When adding a backend, implement the full interface and register it in `supportedBackends()`; do not branch on the backend name from the manager — keep behavior in the driver.

### Up flow

`Manager.Up` is the canonical end-to-end flow worth understanding before changing anything in the lifecycle:

1. Allocate a name (`dev-1`, `dev-2`, …) if not provided.
2. `ensureBackend` → install Multipass if missing (`brew`, `apt`, `winget`, …).
3. If the instance already exists in the registry **or** in the backend, reuse/import it instead of recreating.
4. Generate an ed25519 SSH key under `~/.vpsbox/keys/<name>`.
5. Render a cloud-init YAML to `~/.vpsbox/tmp/<name>-cloud-init.yaml` via `internal/prep` (installs Docker CE, admin tools, writes `/etc/vpsbox-info` as the sandbox marker).
6. `backend.Create` launches Ubuntu.
7. `waitAndRefreshInstance` polls `backend.Info` for up to 20 minutes, and once an IPv4 appears it: writes the managed `# >>> vpsbox >>>` block to `/etc/hosts` (degrades gracefully on `ErrPrivilegesRequired`), provisions a TLS cert covering both the local hostname and the sslip.io wildcard, and upserts the registry record.

### DNS / TLS quirk you must respect

The original plan called for `*.dev-1.vpsbox.local`, but `/etc/hosts` cannot do wildcards. The implementation uses two parallel hostnames per instance, both produced by `domain.NamesForInstance`:

- `<name>.vpsbox.local` — the root hostname, written to `/etc/hosts` (single entry, no wildcard).
- `<name>.<hyphenated-ip>.sslip.io` — the wildcard-safe app base via the public sslip.io DNS service. `*.<hyphenated-ip>.sslip.io` resolves to the VM IP without any local DNS daemon.

`mkcert` certs include both names plus the sslip.io wildcard. If you change naming, update `domain.NamesForInstance`, the cert SAN list in `Manager.refreshInstanceWithTLSPreference`, and `internal/domain/hosts_test.go` together.

### Registry contract (external consumers depend on this)

`~/.vpsbox/instances.json` is a public contract — Server Compass and the `deploytovps` skill read it directly. The fields documented in `docs/server-compass.md` (`name`, `status`, `host`, `hostname`, `port`, `username`, `private_key_path`, `labels`, `domain_base`, `cert_path`, `cert_key_path`) must keep their JSON tags stable. `vpsbox export --format json|sc|env` is the other half of the contract; don't break field names without updating both `Manager.Export` and the docs in `docs/`.

`registry.currentVersion` is bumped when the on-disk schema changes — handle migration in `Store.LoadInstances` rather than mutating older files in place.

### State layout (under `~/.vpsbox/`, configurable via `VPSBOX_HOME`)

```
instances.json   shares.json   auth.json   known_hosts
keys/<name>      keys/<name>.pub
certs/<name>.pem certs/<name>-key.pem
tmp/<name>-cloud-init.yaml
logs/share-<name>.log
```

The hosts file path is `/etc/hosts` by default but is overridable via `VPSBOX_HOSTS_PATH` — tests rely on this to avoid touching the real system file. Use `config.FromBase` in tests instead of constructing paths manually.

### Desktop app

`desktop/app.go` defines `DesktopApp`, which holds a `vpsapp.Manager` and exposes Wails-bound methods to the React/Vite frontend in `desktop/frontend/` (React 18 + TypeScript + Vite). `installers.go` handles the "install Multipass for me" flow on first launch — that is the desktop app's main reason to exist beyond the CLI. Generated Wails bindings live in `desktop/frontend/wailsjs/` and should not be hand-edited.
