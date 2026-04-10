# Server Compass Integration

## Registry contract

Server Compass should read `~/.vpsbox/instances.json`. Each record contains:

- `name`
- `status`
- `host`
- `hostname`
- `port`
- `username`
- `private_key_path`
- `labels`
- `domain_base`
- `cert_path`
- `cert_key_path`

## Intended SC changes

The implementation here assumes the SC desktop app adds the small integration work from the plan:

1. Read the registry file and surface a `Local Sandbox` path in Add Server.
2. Treat `/etc/vpsbox-info` as the sandbox marker after initial SSH connect.
3. Skip public DNS verification for `*.vpsbox.local` and `*.sslip.io`.
4. Prefer the pre-generated local certificate instead of Let's Encrypt when the server is a sandbox.
5. Exempt sandbox servers from the free-tier server limit.

## Manual workflow today

Until the SC-side UI ships, the current path is:

```bash
vpsbox up
vpsbox export dev-1
```

Paste the exported JSON into the Server Compass import flow or use the printed fields manually.
