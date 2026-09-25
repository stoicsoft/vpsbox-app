# HTTP reverse-proxy compatibility R1 preflight

This lab prepares three Ubuntu 24.04 VPSBox guests for local Server Compass
preflight:

- `nginx-default`: the Debian-family `sites-available`/`sites-enabled` layout
  with only the packaged-default path enabled.
- `nginx-foreign`: the same layout plus a byte-checksummed foreign vhost.
- `caddy`: a package-managed systemd Caddy service with a clean
  `/etc/caddy/Caddyfile` and no Server Compass import.

Every guest also has a fixture-owned HTTP upstream on `127.0.0.1:4101`, a
safe port-change helper, and a `certbot` stub that accepts only
`certonly --webroot` and writes short-lived self-signed files under the normal
`/etc/letsencrypt/live/<domain>/` paths. It records calls in
`/var/log/vpsbox-fixtures/certbot.log` and rejects `--nginx`.

This is an Ubuntu preflight, not Debian 12 release acceptance. VPSBox currently
supports Multipass Ubuntu images `22.04` and `24.04`; the stock Multipass image
catalog does not provide Debian, and VPSBox cloud-init uses the Ubuntu Docker
repository.

## Create and export the lab

From `vpsbox-core/`:

```bash
go run ./cmd/vpsbox lab validate \
  --file scenarios/http-reverse-proxy-compat/r1.yaml
go run ./cmd/vpsbox lab apply \
  --file scenarios/http-reverse-proxy-compat/r1.yaml \
  --run-id sc-proxy-r1
go run ./cmd/vpsbox lab status sc-proxy-r1
go run ./cmd/vpsbox lab export sc-proxy-r1
```

The concrete Multipass names are:

- `sc-proxy-r1-nginx-default`
- `sc-proxy-r1-nginx-foreign`
- `sc-proxy-r1-caddy`

## Baseline checks and state switches

```bash
go run ./cmd/vpsbox ssh sc-proxy-r1-nginx-default -- \
  sudo vpsbox-nginx-fixture-check
go run ./cmd/vpsbox ssh sc-proxy-r1-nginx-foreign -- \
  sudo nginx -T
go run ./cmd/vpsbox ssh sc-proxy-r1-caddy -- \
  sudo vpsbox-caddy-fixture-check
```

Move the loopback upstream, then redeploy the Server Compass app with the new
host port and verify its owned route was rewritten:

```bash
go run ./cmd/vpsbox ssh sc-proxy-r1-nginx-default -- \
  sudo vpsbox-upstream-set-port 4102 redeploy-4102
go run ./cmd/vpsbox ssh sc-proxy-r1-nginx-default -- \
  curl --fail --resolve app.example.test:80:127.0.0.1 \
  http://app.example.test/
```

Seed the historical Caddy corruption while leaving the active in-memory route
running:

```bash
go run ./cmd/vpsbox ssh sc-proxy-r1-caddy -- \
  sudo vpsbox-caddy-set-state literal-newline
```

Server Compass should repair the literal `\nimport...`, install exactly one
durable `import /etc/caddy/server-compass/*.caddy`, validate, and reload.
Restore the clean baseline with the lab-owned snapshot before another case:

```bash
go run ./cmd/vpsbox lab reset sc-proxy-r1 --snapshot seeded-baseline
```

For nginx rollback testing, capture the managed vhost bytes and live response,
attempt a malformed replacement, and verify both are unchanged after the
configtest failure. Because nginx is already active, a failed
`systemctl reload nginx` must be surfaced; a later successful
`systemctl start nginx` is not evidence that the reload worked.

The foreign vhost contains
`VPSBOX_FOREIGN_VHOST_SENTINEL=preserve-unless-explicitly-adopted` and its
baseline SHA-256 at `/etc/vpsbox-http-proxy/foreign-vhost.sha256`. A refused
adoption must leave those bytes unchanged.
