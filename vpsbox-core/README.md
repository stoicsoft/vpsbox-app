# vpsbox

`vpsbox` is the local Ubuntu VPS sandbox for trying deploy tools before you rent a server.

It wraps a VM backend, prepares the guest to look like a fresh Ubuntu VPS, writes a local registry at `~/.vpsbox/instances.json`, bootstraps SSH keys and TLS assets, and exposes a command surface designed for Server Compass, deploytovps-style skills, and self-hosted PaaS tools.

## Problem

Trying a deploy tool usually means renting a VPS too early, wiring up SSH, DNS, TLS, and Docker by hand, and then breaking a real server while you are still learning the workflow.

That makes it expensive and slow to evaluate tools like Server Compass, Coolify, Dokploy, Dokku, CapRover, or Kamal, especially if you only want a realistic Ubuntu box to test against.

## Solution

`vpsbox` gives you a local Ubuntu VM that behaves like a fresh VPS, but runs on your own machine.

You can install the required host tooling, create a sandbox, SSH into it, open local HTTPS URLs, and throw it away when you are done. The goal is to let you practice the real deploy flow locally before you spend money or touch production infrastructure.

## Who should use this tool

`vpsbox` is for:

- Developers evaluating deploy tools before renting a real VPS
- People learning Linux server workflows, SSH, Docker, domains, and HTTPS
- Server Compass users who want a safe local sandbox before managing production servers
- Anyone building tutorials, demos, or repeatable self-hosting test environments on a laptop

`vpsbox` is probably not the right tool if you need a real internet-facing production server, multi-node infrastructure, or a cloud environment that exactly matches your final hosting provider.



## Install

### Desktop app

If you want the easiest path, start with the desktop app.

One-line install:

macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/stoicsoft/vpsbox/main/scripts/install.sh | bash
```

Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/stoicsoft/vpsbox/main/scripts/install.sh | bash
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/stoicsoft/vpsbox/main/scripts/install.ps1 | iex
```

If you already downloaded a desktop build or installer, you can skip the source-build steps below.

If you are building it from source on a clean machine, install these first:

- Git from [git-scm.com/downloads](https://git-scm.com/downloads)
- Go 1.25 or newer from [go.dev/doc/install](https://go.dev/doc/install)
- Node.js LTS and npm from [nodejs.org/en/download](https://nodejs.org/en/download)
- Wails CLI 2.12.0:

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
```

Install Go first:

- macOS: use the official installer from `go.dev`, or `brew install go` if you already use Homebrew.
- Linux: use the official `go.dev` install guide, or a distro package that provides Go 1.25 or newer.
- Windows: use the official MSI installer from `go.dev`.

Verify Go is installed:

```bash
go version
```

Clone the repo:

```bash
git clone https://github.com/stoicsoft/vpsbox.git
cd vpsbox
```

Then build the desktop app from the repo root.

macOS:

```bash
./scripts/build-desktop.sh
./scripts/package-desktop-macos.sh
```

Linux:

```bash
./scripts/build-desktop-linux.sh
```

Windows PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-desktop-windows.ps1
```

Desktop outputs by platform:

```text
macOS app:      dist/vpsbox.app
macOS installer: dist/vpsbox-installer.pkg
Linux:          dist/vpsbox-linux
Windows:        dist/vpsbox.exe
```

The Wails app lives in [`desktop/README.md`](desktop/README.md).

### If you're not familiar with terminal commands

Use the desktop app instead of the CLI. It walks you through installing Multipass, `mkcert`, and `cloudflared`, then lets you create, start, stop, resize, and delete sandboxes from buttons.

### CLI

If you want the command line, install Git and Go 1.25 or newer first. You do not need Node.js or Wails for the CLI build.

macOS and Linux:

```bash
git clone https://github.com/stoicsoft/vpsbox.git
cd vpsbox
go build -o bin/vpsbox ./cmd/vpsbox
```

Windows PowerShell:

```powershell
git clone https://github.com/stoicsoft/vpsbox.git
cd vpsbox
go build -o bin\vpsbox.exe .\cmd\vpsbox
```

## Using the desktop app

After building or downloading the desktop app, launch the build for your platform:

- macOS: open `dist/vpsbox.app`
- Linux: run `./dist/vpsbox-linux`
- Windows: run `.\dist\vpsbox.exe`

1. On first launch, open the setup flow and click `Install required packages`. The app installs Multipass for the VM, `mkcert` for trusted HTTPS, and `cloudflared` for sharing. Expect an admin prompt while those packages are installed.
2. Create a sandbox from the `Servers` screen. You can leave the name empty to auto-generate one such as `dev-1`, and the default size is 2 vCPU, 2 GB RAM, and 10 GB disk.
3. Select a server in the sidebar to manage it. The desktop app exposes `Overview`, `Connect`, and `Resources` tabs for status, browser URL, SSH commands, resize, start/stop, and delete actions.
4. From `Connect`, generate an SSH key if needed, open the sandbox URL in your browser, or copy the generated SSH/SCP commands. In this build, `Open shell` is implemented on macOS. On Linux and Windows, copy the SSH command and run it in your terminal.
5. From `System`, use `Fix /etc/hosts` if you need to refresh local hostname routing for `<name>.vpsbox.local`.
6. Check `Activity` for install progress, provisioning steps, and recent job errors.

### Typical usage

```bash
vpsbox doctor
vpsbox up
vpsbox list
vpsbox info dev-1
vpsbox ssh dev-1
vpsbox export dev-1
vpsbox ui
```

## Demo tutorial: deploy a Ghost blog to VPSBox

This demo keeps Ghost simple: one sandbox, one Docker container, and Ghost exposed on port `2368`.

1. Create a sandbox:

```bash
vpsbox doctor
vpsbox up --name ghost-demo
```

2. SSH into it:

```bash
vpsbox ssh ghost-demo
```

3. Start Ghost inside the sandbox:

```bash
docker volume create ghost-data
docker run -d \
  --name ghost \
  --restart unless-stopped \
  -p 2368:2368 \
  -e url=http://ghost-demo.vpsbox.local:2368 \
  -v ghost-data:/var/lib/ghost/content \
  ghost:5-alpine
```

4. Open the blog from your host machine:

```text
http://ghost-demo.vpsbox.local:2368
```

If `ghost-demo.vpsbox.local` does not resolve on your machine yet, run `vpsbox info ghost-demo` and use the `Wildcard base` value instead:

```text
http://ghost-demo.<wildcard-base>:2368
```

5. Open the Ghost admin:

```text
http://ghost-demo.vpsbox.local:2368/ghost
```

6. Remove the demo when you are done:

```bash
vpsbox destroy ghost-demo --force
```

This example uses plain HTTP on port `2368`. If you want HTTPS or a cleaner hostname, put Ghost behind a reverse proxy inside the sandbox.

## Command surface

```text
vpsbox up [--name NAME] [--cpus N] [--memory N] [--disk N]
vpsbox down [NAME]
vpsbox destroy [NAME] [--force]
vpsbox list [--json]
vpsbox info [NAME] [--json]
vpsbox ssh [NAME] [-- COMMAND...]
vpsbox snapshot [NAME] [--name SNAPSHOT]
vpsbox reset [NAME] [--snapshot SNAPSHOT]
vpsbox snapshots [NAME]
vpsbox export [NAME] [--format json|sc|env]
vpsbox doctor [--json]
vpsbox ui [--port 7878]
vpsbox share URL [--ttl 4h] [--name SLUG]
vpsbox shares
vpsbox unshare NAME
vpsbox login --email EMAIL [--token TOKEN] [--api-base URL]
vpsbox logout
vpsbox upgrade
vpsbox version
```

## How it works

1. `vpsbox up` detects a VM backend, installs Multipass if needed, generates an SSH key, renders cloud-init, and launches Ubuntu.
2. The cloud-init script installs Docker CE, common admin tools, and writes `/etc/vpsbox-info`.
3. After boot, `vpsbox` reads the guest IP, writes a managed section to `/etc/hosts`, generates TLS material, and stores the instance in `~/.vpsbox/instances.json`.
4. `vpsbox export` exposes a small import contract that other tools can consume without any RPC or daemon.

## Docs

- [Architecture](docs/architecture.md)
- [Desktop app guide](desktop/README.md)
- [Server Compass integration contract](docs/server-compass.md)
- [deploytovps skill usage](docs/deploytovps-skill.md) (coming soon)
- [Coolify guide](docs/coolify.md)
- [Dokploy guide](docs/dokploy.md)
- [Dokku guide](docs/dokku.md)
- [CapRover guide](docs/caprover.md)
- [Kamal guide](docs/kamal.md)

## Status

What is implemented here:

- Go-based single-binary CLI
- Wails desktop app with installer-first flow under `desktop/`
- Multipass-first backend detection with Lima fallback placeholder
- `up`, `down`, `destroy`, `list`, `info`, `ssh`, `snapshot`, `reset`, `snapshots`
- `export`, `doctor`, `ui`, `share`, `shares`, `unshare`, `login`, `logout`, `upgrade`, `version`
- Cloud-init based Ubuntu prep
- `~/.vpsbox` registry/state management
- Local dashboard served from the same binary
- Quick tunnel based sharing via `cloudflared`
- Cross-platform compile support for macOS, Linux, and Windows

Current implementation caveats:

- The concrete VM workflow is fully implemented for Multipass. Lima is detected but still returns `ErrUnsupported`.
- Local wildcard DNS is approximated with `sslip.io` because `/etc/hosts` does not support wildcard hostnames. The root instance hostname still uses `<name>.vpsbox.local`.
- The hosted `share.vpsbox.dev` relay is not included. `vpsbox share` currently uses Cloudflare quick tunnels.
- Server Compass integration points are documented in [`docs/server-compass.md`](docs/server-compass.md).