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

`instances.json`, `shares.json`, `buckets.json` (0600 — holds the object store secret), `workspaces.json`, `auth.json`, `known_hosts`, `keys/<name>[.pub]`, `certs/<name>.pem` + `certs/<name>-key.pem`, `tmp/<name>-cloud-init.yaml`, `logs/share-<name>.log`, `logs/update.log`, `logs/objstore.log`, `updates/` (self-update downloads + staging), `objstore/` (`garage.toml` + `meta/` + `data/`). The hosts path is `/etc/hosts` but overridable via `VPSBOX_HOSTS_PATH` (tests rely on this — use `config.FromBase` in tests, don't build paths by hand).

### Object storage (`internal/objstore`, `internal/app/objstore*.go`)

`vpsbox bucket` gives the machine a local S3 endpoint so backup/restore flows can be rehearsed offline. The server (Garage, installed via brew) runs **on the host, not in a sandbox** — a bucket has to outlive the box that wrote to it for `snapshot reset` → restore to mean anything, and it is shared by every sandbox.

Follows the `share.Manager` shape, not `backend.Backend`: ensure a binary, run it detached with `processGroupAttrs()`, track the PID in JSON, reap on read. Three things are easy to break here:

- **Credentials must survive a restart.** `Stop` clears only the PID, never the keys. Attached sandboxes hold the access key in their `~/.aws/credentials`, and Garage scopes bucket listings *per key* — minting a new key both breaks those sandboxes and hides every existing bucket. `ListBuckets` is additive for the same reason: a bucket missing from a listing may just be invisible to this key, so records are never deleted on a read.
- **`s3.go` is a hand-rolled SigV4 client** (the module has only cobra + yaml as deps). `s3_test.go` covers it against a stub; `s3_live_test.go` covers it against a real server and is skipped unless `VPSBOX_OBJSTORE_E2E_ENDPOINT` is set. Run the live one after touching signing — the stub cannot catch a wrong-but-consistent signature.
- **`AttachBuckets` discovers the host address from inside the VM** (`ip route show default`) rather than guessing per platform, then writes a fenced `/etc/hosts` block, `~/.aws/credentials` (0600), endpoint-only `profile.d`, and a baked-endpoint `s3` wrapper. Secrets must stay out of `profile.d` (world readable) and the endpoint must stay baked into the wrapper (`profile.d` is not sourced for `ssh box 'cmd'`).

v1 serves plain HTTP: `prep/cloudinit.go` installs `ca-certificates` but never trusts the mkcert root CA, so HTTPS here would only teach people `--no-verify-ssl`. The S3 port binds `0.0.0.0` (the sandbox reaches it over the hypervisor bridge); RPC and admin stay on loopback. Not available on Windows — Garage publishes no build for it.

### Private networks (`internal/workspace`, `internal/app/workspace*.go`)

`vpsbox workspace` groups sandboxes onto a private network, after a cloud provider's model: one `/24` per workspace out of `10.42.0.0/16`, a stable second address per member, members reachable by name, different workspaces mutually unreachable.

Connectivity between sandboxes is **not** what this adds — every Multipass VM already shares one L2 bridge and can already ping the others. What it adds is stable addressing (DHCP leases move), naming, and isolation. `internal/workspace` is pure (allocation + config rendering, fully unit-tested); `internal/app/workspace*.go` applies it over SSH.

Two things here are easy to get wrong:

- **The netplan drop-in must be keyed by netplan's *definition* name, not the interface name.** The stock cloud image declares its NIC as `ethernets: {default: {match: {macaddress: ...}, dhcp4: true}}` — the key is `default`, the interface is `enp0s1`. Netplan merges by key, so a drop-in keyed `enp0s1` is a second definition claiming the same NIC. It is accepted with **exit 0 and no warning**, emits two systemd-networkd files, and only the lexically first is applied — so the address silently never appears (or, if the names sorted the other way, DHCP is lost and the sandbox goes unreachable). `workspaceProbeScript` discovers the real key with `netplan get ethernets`. Validate changes here with `netplan generate --root-dir <tmp>`, which is non-destructive.
- **The firewall lives in its own `inet vpsbox_workspace` table with an accept policy**, so applying it never takes responsibility for the user's other rules, and re-applying can flush just our table. Sandboxes running Docker already have `ip nat`/`ip filter`/`ip6 …` tables; ours is a different family and name, so they coexist. `nft` rules are not persistent, so `vpsbox-workspace-fw.service` reloads them at boot.
- **The names block needs restoring at boot, the address does not.** The cloud image sets cloud-init's `manage_etc_hosts`, and `update_etc_hosts` runs every boot, regenerating `/etc/hosts` and taking our block with it — so the address and firewall survive a restart but the names would silently stop resolving. `vpsbox-workspace-hosts.service` re-applies the saved block, ordered `After=cloud-init.service`.

Two things that will waste your time when verifying workspace DNS by hand:

- `systemd-resolved` caches, so `getent hosts` happily returns stale answers after you edit `/etc/hosts`. Run `resolvectl flush-caches` between checks or you will conclude something works when it does not.
- Both boot units are `Type=oneshot` with `RemainAfterExit=yes`, so once active, `systemctl start` is a **no-op**. Use `systemctl restart` to simulate a fresh boot; `start` silently does nothing.

The isolation is cooperative, not a security boundary — the sandbox is root on itself and can flush the rules. Say so rather than implying otherwise; it is also true of a real provider's private network. Cross-workspace traffic additionally fails at L3 (no address in the other subnet) before the firewall is consulted.

### Real-VPS migration (`internal/migrate`, `internal/app/transfer*.go`)

`vpsbox push` (sandbox → real VPS) and `vpsbox import` (real VPS → fresh sandbox) share one engine. It migrates *work*, never the *machine*: manual apt packages (delta against what the destination already has), a curated path set (`/root`, `/home`, `/opt`, `/srv`, `/var/www`, `/usr/local`, service configs under `/etc`), docker volume data, systemd enablement, and running compose projects. `internal/migrate` is pure (script builders + parsers + plan computation, fully unit-tested); `internal/app/transfer.go` runs it over SSH. The host relays every byte (`ssh src tar -c | ssh dst tar -x` piped through the process), so the two machines never need to reach each other, and cross-architecture works because packages install from the destination's own apt and docker images are pulled there.

Things to preserve when touching this:

- **The tar excludes are the safety contract.** `*/.ssh`, `etc/ssh`, network/firewall/hostname config, and `etc/systemd/system/*.wants` never move, in either direction, excluded at create *and* extract. The wants exclusion matters doubly: enablement symlinks travelling by tar would silently enable services — enabling is done deliberately, unit by unit, in `ServiceScript`, behind a skip-list of machine-level units (guest agents, ssh, ufw, systemd-*).
- **apt sources sync before package install** (`aptPaths` vs `dataPaths`) so third-party packages — docker-ce above all — resolve on the destination.
- **Failure severity is deliberate**: per-package/service/compose failures are `##…FAIL##` marker lines collected as warnings; a failed docker-volume copy is a hard error (that's user data). Tar create tolerates exit 1 (live filesystems change mid-read).
- Desktop surface: `desktopbackend/transfer.go` (`PreviewPush` sync + `StartPushToVPS`/`StartImportFromVPS` jobs). The frontend must always show `PreviewPush`'s plan before offering the push button.

### Lab / scenario feature (`internal/scenario`, `internal/app/lab*.go`)

`vpsbox lab` orchestrates reproducible **multi-VPS** labs from a versioned YAML manifest (`apiVersion: vpsbox.stoicsoft.com/v1alpha1`, `kind: Lab`). `scenario.Load`/`Manifest.Validate` strictly validate manifests (bounded sizes, `KnownFields(true)`, allow-listed fixtures, safe-name regex). A lab run is keyed by an exact lowercase `run-id`; VMs are named `<run-id>-<logical-name>` and destroy is scoped to only VMs recorded as owned by that run. Subcommands: `validate`, `apply`, `status`, `snapshot`, `reset`, `export`, `destroy`. Example manifests live in `vpsbox-core/scenarios/`.

### Desktop backend: `vpsbox-core/desktopbackend`

`desktopbackend.App` wraps a `Manager` and exposes an **async, job-based** API: the `Start*` methods return a job ID immediately and run the work in a goroutine, streaming progress into an in-memory `jobs` map that the frontend polls via `GetState()`. `installers*.go` handles the first-launch "install Multipass/mkcert/cloudflared for me" flow — the desktop app's main reason to exist beyond the CLI. `OpenShell` and `RevealKeyFolder` are currently macOS-only.

**Self-update** (`update.go` + `selfupdate*.go`) is the one flow that reaches outside the process. `update.go` polls the GitHub releases API and picks the asset matching this OS/arch from the names `.github/workflows/build.yml` publishes — **renaming a release artifact breaks `selectUpdateAsset`, so change both together**. `selfupdate.go` downloads and stages it; each platform's `selfupdate_<os>.go` supplies `canSelfUpdate` / `stageUpdate` / `launchUpdateSwap`. The swap can't happen in-process (you can't replace a running app from inside it), so `launchUpdateSwap` writes a detached helper script that waits for the app's PID to exit, replaces it, and relaunches — the root `DesktopApp.ApplyUpdate` quits the app right after. Every failure path in those scripts must leave a launchable app behind; `selfupdate_darwin_test.go` executes them against fake bundles to prove it.

### Frontend: `frontend/`

React 18 + TypeScript + Vite, essentially one big `src/App.tsx`. Generated Wails bindings live in `frontend/wailsjs/` — **do not hand-edit them**; they regenerate from the root `app.go` bound methods on `wails dev`/`build`.

## Gotchas / stale artifacts

- **`vpsbox-core/CLAUDE.md` describes an older, pre-merge layout** (where `desktop/` was a submodule inside the core repo). Trust *this* file for the current structure; that one is still useful for deep core-package detail but its paths and module names are out of date.
- **`vpsbox-core/desktop/` is the superseded standalone desktop app** (its own `go.mod`). The active desktop app is at the repo root. Don't edit `vpsbox-core/desktop/` expecting the shipped app to change.
- The root `AGENTS.md` and `.agents/` are scratch/junk — ignore them.
