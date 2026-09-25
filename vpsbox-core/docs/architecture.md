# Architecture

## Core pieces

- `cmd/vpsbox`: CLI entrypoint
- `internal/app`: orchestration layer, commands, local dashboard
- `internal/backend`: VM backend abstraction and Multipass implementation
- `internal/prep`: cloud-init generation for VPS preparation
- `internal/registry`: persistent local state under `~/.vpsbox`
- `internal/domain`: host file management and instance naming
- `internal/tls`: `mkcert` integration with self-signed fallback
- `internal/share`: `cloudflared` quick tunnel management
- `internal/doctor`: host diagnostics

## State layout

```text
~/.vpsbox/
  instances.json
  shares.json
  auth.json
  keys/
  certs/
  logs/
  tmp/
```

## Backend model

`vpsbox` chooses a backend by priority:

1. Multipass
2. Lima

Only Multipass is implemented end to end right now. Lima is a placeholder for the later fallback path from the plan.

## DNS and TLS

The design plan called for `*.dev-1.vpsbox.local`, but `/etc/hosts` does not support wildcard entries. This implementation therefore uses:

- `<name>.vpsbox.local` for root instance access
- `<name>.<hyphenated-ip>.sslip.io` as the wildcard-safe app base
- `mkcert` certificates that include both the local hostname and the `sslip.io` wildcard base

That preserves the same hostname-first workflow while keeping wildcard routing practical without a local DNS daemon.
