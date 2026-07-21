# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`vpsbox-desktop` is the Wails desktop app for VPSBox. VPSBox boots a local Ubuntu VM (via Multipass) and presents it as a fresh "VPS" for trying deploy tools (Server Compass, Coolify, Dokploy, Dokku, CapRover, Kamal, etc.) without renting a real server.

This repo is self-contained: the native Wails shell + React frontend live at the root, and the entire VPS engine core is vendored under `vpsbox-core/` as a **separate Go module** pulled in with a local `replace` directive. The desktop app is a thin shell over the core's orchestration layer — the same code the standalone CLI uses.

## Two Go modules

The `replace` in the root `go.mod` (`replace github.com/stoicsoft/vpsbox => ./vpsbox-core`) is what glues them:

1. **Root module** `github.com/stoicsoft/vpsbox-desktop` — the Wails shell. Just `main.go` (Wails runtime bootstrap) + `app.go` (thin binding layer that forwards to `desktopbackend`). Do not put logic here.
2. **Core module** `github.com/stoicsoft/vpsbox` (in `vpsbox-core/`) — the CLI (`cmd/vpsbox`), the reusable engine (`internal/`), and the desktop backend (`desktopbackend/`). All real behavior lives here.

**Never fork logic into the root module.** New capabilities go in `vpsbox-core/internal/app` (as a `Manager` method), and are then exposed two ways: a Cobra subcommand in `internal/app/cli.go` (for the CLI) and a `desktopbackend` method surfaced through the root `app.go` (for the desktop UI).

## Common commands

```bash
# Dev (from repo root): go mod tidy + npm install + wails dev
./scripts/dev.sh

# Production desktop build → build/bin/  (embeds Version via ldflags)
./scripts/build.sh                 # VERSION=1.2.3 ./scripts/build.sh to stamp a version

# Raw Wails commands (wails installs to $(go env GOPATH)/bin)
$(go env GOPATH)/bin/wails dev
$(go env GOPATH)/bin/wails build -clean

# Frontend only
cd frontend && npm install && npm run build   # tsc + vite build
```

### Tests live in the core module, not the root

The root module is just the Wails `main` package — `go test ./...` there covers almost nothing. Run tests from `vpsbox-core/`:

```bash
cd vpsbox-core
go test ./...

# Cross-compile checks matter — CI builds macOS/Linux/Windows. Keep all three green.
GOOS=linux   GOARCH=amd64 go build ./...
GOOS=windows GOARCH=amd64 go build ./...

# Single package / single test
go test ./internal/registry
go test ./internal/scenario -run TestManifest_Validate

# gofmt is enforced culture here — keep the tree formatted
gofmt -l .
```

Host-specific code is split with build tags via `_darwin.go` / `_linux.go` / `_windows.go` filename suffixes (see `desktopbackend/` and `internal/backend`). Follow that pattern instead of `runtime.GOOS` branching when adding platform behavior.

## CI

The **only** active CI is the root `.github/workflows/build.yml`. It builds signed/notarized artifacts for macOS (DMG + zip), Windows (NSIS installer), and Linux (tarball) on tag pushes (`v*`) and PRs, and drafts a GitHub Release on tags or manual `publish_release`. Version flows in through `-ldflags "-X 'github.com/stoicsoft/vpsbox/internal/app.Version=<v>'"`.

The workflow files under `vpsbox-core/.github/workflows/` are **inert** — GitHub only reads workflows from the repo root, so the vendored core's own `ci.yml`/`release.yml` never fire here.

## Architecture

### Orchestration layer: `vpsbox-core/internal/app`

`internal/app.Manager` (constructed via `NewManager(ctx)`) is the single entry point for both the CLI and the desktop app. It owns `config.Paths` (every `~/.vpsbox/...` path — never hardcode one), `registry.Store` (JSON-on-disk state), `tls.Manager` (mkcert with self-signed fallback), `share.Manager` (cloudflared quick tunnels), and a `backend.Backend` VM driver chosen by `backend.Detect`.

`Manager.Up` is the canonical end-to-end lifecycle (allocate name → ensure/install Multipass → reuse-or-import if it already exists → generate ed25519 key → render cloud-init via `internal/prep` → `backend.Create` → poll `backend.Info` up to ~20 min, then write `/etc/hosts`, provision the TLS cert, and upsert the registry). Read it before changing anything in the create/start path.

### Backend abstraction: `vpsbox-core/internal/backend`

`Backend` is the VM-driver interface. `Detect()` picks one by `Priority()` from `supportedBackends()` (OS-gated). **Multipass is the only working backend; Lima is a stub returning `ErrUnsupported`.** Add a driver by implementing the full interface and registering it in `supportedBackends()` — never branch on backend name from the `Manager`.

### DNS / TLS quirk (respect this)

`/etc/hosts` can't do wildcards, so each instance gets two parallel hostnames from `domain.NamesForInstance`: `<name>.vpsbox.local` (single `/etc/hosts` entry) and `<name>.<hyphenated-ip>.sslip.io` (wildcard-safe app base via public sslip.io DNS, no local daemon). mkcert certs cover both plus the sslip.io wildcard. Changing naming means updating `domain.NamesForInstance`, the cert SAN list in `Manager.refreshInstanceWithTLSPreference`, and `internal/domain/hosts_test.go` together.

### Registry contract (external consumers depend on this)

`~/.vpsbox/instances.json` is a public contract — Server Compass and the `deploytovps` skill read it directly. JSON field tags documented in `vpsbox-core/docs/server-compass.md` (`name`, `status`, `host`, `hostname`, `port`, `username`, `private_key_path`, `labels`, `domain_base`, `cert_path`, `cert_key_path`) must stay stable. `vpsbox export --format json|sc|env` is the other half — don't rename fields without updating both `Manager.Export` and `docs/`. Schema changes bump `registry.currentVersion` (currently 2) and migrate in `Store.LoadInstances`, not by mutating old files in place.

### State layout (under `~/.vpsbox/`, overridable via `VPSBOX_HOME`)

`instances.json`, `shares.json`, `auth.json`, `known_hosts`, `keys/<name>[.pub]`, `certs/<name>.pem` + `certs/<name>-key.pem`, `tmp/<name>-cloud-init.yaml`, `logs/share-<name>.log`. The hosts path is `/etc/hosts` but overridable via `VPSBOX_HOSTS_PATH` (tests rely on this — use `config.FromBase` in tests, don't build paths by hand).

### Lab / scenario feature (`internal/scenario`, `internal/app/lab*.go`)

`vpsbox lab` orchestrates reproducible **multi-VPS** labs from a versioned YAML manifest (`apiVersion: vpsbox.stoicsoft.com/v1alpha1`, `kind: Lab`). `scenario.Load`/`Manifest.Validate` strictly validate manifests (bounded sizes, `KnownFields(true)`, allow-listed fixtures, safe-name regex). A lab run is keyed by an exact lowercase `run-id`; VMs are named `<run-id>-<logical-name>` and destroy is scoped to only VMs recorded as owned by that run. Subcommands: `validate`, `apply`, `status`, `snapshot`, `reset`, `export`, `destroy`. Example manifests live in `vpsbox-core/scenarios/`.

### Desktop backend: `vpsbox-core/desktopbackend`

`desktopbackend.App` wraps a `Manager` and exposes an **async, job-based** API: the `Start*` methods return a job ID immediately and run the work in a goroutine, streaming progress into an in-memory `jobs` map that the frontend polls via `GetState()`. `installers*.go` handles the first-launch "install Multipass/mkcert/cloudflared for me" flow — the desktop app's main reason to exist beyond the CLI. `OpenShell` and `RevealKeyFolder` are currently macOS-only.

### Frontend: `frontend/`

React 18 + TypeScript + Vite, essentially one big `src/App.tsx`. Generated Wails bindings live in `frontend/wailsjs/` — **do not hand-edit them**; they regenerate from the root `app.go` bound methods on `wails dev`/`build`.

## Gotchas / stale artifacts

- **`vpsbox-core/CLAUDE.md` describes an older, pre-merge layout** (where `desktop/` was a submodule inside the core repo). Trust *this* file for the current structure; that one is still useful for deep core-package detail but its paths and module names are out of date.
- **`vpsbox-core/desktop/` is the superseded standalone desktop app** (its own `go.mod`). The active desktop app is at the repo root. Don't edit `vpsbox-core/desktop/` expecting the shipped app to change.
- The root `AGENTS.md` and `.agents/` are scratch/junk — ignore them.
