vpsbox_apt install -y --no-install-recommends caddy

cat >/usr/local/bin/vpsbox-caddy-set-state <<'SH'
#!/usr/bin/env bash
set -euo pipefail

state="${1:-}"
case "$state" in
  clean)
    cat >/etc/caddy/Caddyfile <<'EOF'
:80 {
    respond "vpsbox caddy baseline\n" 200
}
EOF
    caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
    systemctl enable caddy >/dev/null
    systemctl restart caddy
    ;;
  literal-newline)
    # Deliberately put backslash+n in the file. Do not reload: the active
    # baseline route remains available while repair behavior is exercised.
    printf '%s' ':80 {
    respond "vpsbox caddy baseline\n" 200
}\nimport /etc/caddy/server-compass/*.caddy
' >/etc/caddy/Caddyfile
    grep -Fq '\nimport /etc/caddy/server-compass/*.caddy' /etc/caddy/Caddyfile
    ;;
  *)
    echo "usage: vpsbox-caddy-set-state clean|literal-newline" >&2
    exit 64
    ;;
esac

printf '%s\n' "$state" >/etc/vpsbox-http-proxy/caddy-state
SH
chmod 0755 /usr/local/bin/vpsbox-caddy-set-state

cat >/usr/local/bin/vpsbox-caddy-fixture-check <<'SH'
#!/usr/bin/env bash
set -euo pipefail

state="$(cat /etc/vpsbox-http-proxy/caddy-state)"
systemctl is-active --quiet caddy
ss -H -ltn 'sport = :80' | grep -q .
curl --fail --silent --show-error http://127.0.0.1/ |
  grep -q 'vpsbox caddy baseline'
curl --fail --silent --show-error http://127.0.0.1:4101/fixture-check |
  grep -q 'port=4101'

if [[ "$state" == "clean" ]]; then
  caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
  if grep -Fq 'import /etc/caddy/server-compass/*.caddy' /etc/caddy/Caddyfile; then
    echo "clean fixture unexpectedly contains the Server Compass import" >&2
    exit 1
  fi
else
  grep -Fq '\nimport /etc/caddy/server-compass/*.caddy' /etc/caddy/Caddyfile
fi

printf 'caddy fixture %s is ready\n' "$state"
SH
chmod 0755 /usr/local/bin/vpsbox-caddy-fixture-check

vpsbox-caddy-set-state clean
vpsbox-caddy-fixture-check
