set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

vpsbox_apt() {
  local attempt
  for attempt in 1 2 3 4 5; do
    if apt-get \
      -o DPkg::Lock::Timeout=120 \
      -o Acquire::Retries=5 \
      "$@"; then
      return 0
    fi
    echo "apt attempt ${attempt} failed; retrying fixture package operation" >&2
    sleep "$((attempt * 2))"
  done
  return 1
}

cloud-init status --wait >/dev/null || true
vpsbox_apt update -qq
vpsbox_apt install -y --no-install-recommends curl openssl python3

install -d -m 0755 /etc/vpsbox-http-proxy
install -d -m 0755 /usr/local/lib/vpsbox-fixtures
install -d -m 0755 /var/log/vpsbox-fixtures

cat >/usr/local/lib/vpsbox-fixtures/http_upstream.py <<'PY'
#!/usr/bin/env python3
import argparse
import http.server
import pathlib


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        marker_path = pathlib.Path("/etc/vpsbox-http-proxy/upstream-marker")
        marker = marker_path.read_text(encoding="utf-8").strip()
        body = (
            f"vpsbox-http-proxy-fixture\n"
            f"port={self.server.server_port}\n"
            f"marker={marker}\n"
            f"path={self.path}\n"
        ).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format, *args):
        return


parser = argparse.ArgumentParser()
parser.add_argument("--port", type=int, required=True)
args = parser.parse_args()
server = http.server.ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
server.serve_forever()
PY
chmod 0755 /usr/local/lib/vpsbox-fixtures/http_upstream.py

cat >/etc/vpsbox-http-proxy/upstream.env <<'EOF'
UPSTREAM_PORT=4101
EOF
printf '%s\n' 'initial-4101' >/etc/vpsbox-http-proxy/upstream-marker

cat >/etc/systemd/system/vpsbox-http-upstream.service <<'EOF'
[Unit]
Description=VPSBox HTTP reverse-proxy compatibility upstream
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=www-data
Group=www-data
EnvironmentFile=/etc/vpsbox-http-proxy/upstream.env
ExecStart=/usr/bin/python3 /usr/local/lib/vpsbox-fixtures/http_upstream.py --port ${UPSTREAM_PORT}
Restart=on-failure
RestartSec=1
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadOnlyPaths=/etc/vpsbox-http-proxy

[Install]
WantedBy=multi-user.target
EOF

cat >/usr/local/bin/vpsbox-upstream-set-port <<'SH'
#!/usr/bin/env bash
set -euo pipefail

port="${1:-}"
marker="${2:-port-${port}}"
if [[ ! "$port" =~ ^[0-9]+$ ]] || (( port < 1024 || port > 65535 )); then
  echo "usage: vpsbox-upstream-set-port PORT [MARKER] (PORT must be 1024-65535)" >&2
  exit 64
fi
if ss -H -ltn "sport = :${port}" | grep -q .; then
  current_port="$(sed -n 's/^UPSTREAM_PORT=//p' /etc/vpsbox-http-proxy/upstream.env)"
  if [[ "$current_port" != "$port" ]]; then
    echo "port ${port} is already in use" >&2
    exit 1
  fi
fi

tmp="$(mktemp /etc/vpsbox-http-proxy/upstream.env.XXXXXX)"
printf 'UPSTREAM_PORT=%s\n' "$port" >"$tmp"
chmod 0644 "$tmp"
mv -f "$tmp" /etc/vpsbox-http-proxy/upstream.env
printf '%s\n' "$marker" >/etc/vpsbox-http-proxy/upstream-marker
systemctl restart vpsbox-http-upstream.service

for _ in $(seq 1 30); do
  if curl --fail --silent "http://127.0.0.1:${port}/ready" >/dev/null; then
    printf 'upstream ready on 127.0.0.1:%s (%s)\n' "$port" "$marker"
    exit 0
  fi
  sleep 0.2
done
echo "upstream did not become ready on 127.0.0.1:${port}" >&2
exit 1
SH
chmod 0755 /usr/local/bin/vpsbox-upstream-set-port

cat >/usr/local/bin/certbot <<'SH'
#!/usr/bin/env bash
set -euo pipefail

install -d -m 0755 /var/log/vpsbox-fixtures
{
  date -u +'%Y-%m-%dT%H:%M:%SZ'
  printf ' %q' "$@"
  printf '\n'
} >>/var/log/vpsbox-fixtures/certbot.log

if [[ "${1:-}" == "--version" ]]; then
  echo "certbot 99.0-vpsbox-webroot-stub"
  exit 0
fi
if [[ "${1:-}" == "certificates" ]]; then
  find /etc/letsencrypt/live -mindepth 1 -maxdepth 1 -type d -print 2>/dev/null || true
  exit 0
fi

domain=""
webroot=""
uses_webroot=false
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
  arg="${args[$i]}"
  case "$arg" in
    --nginx)
      echo "the VPSBox certbot fixture intentionally refuses --nginx" >&2
      exit 65
      ;;
    --webroot)
      uses_webroot=true
      ;;
    -d|--domains|--cert-name)
      ((i += 1))
      domain="${args[$i]:-}"
      ;;
    --domains=*|--cert-name=*)
      domain="${arg#*=}"
      ;;
    -w|--webroot-path)
      ((i += 1))
      webroot="${args[$i]:-}"
      ;;
    --webroot-path=*)
      webroot="${arg#*=}"
      ;;
  esac
done

if [[ "${args[0]:-}" != "certonly" ]] || [[ "$uses_webroot" != true ]]; then
  echo "the VPSBox certbot fixture only supports: certbot certonly --webroot ..." >&2
  exit 64
fi
if [[ ! "$domain" =~ ^[A-Za-z0-9.-]+$ ]] || [[ -z "$webroot" ]] || [[ "$webroot" != /* ]]; then
  echo "a safe domain and absolute webroot are required" >&2
  exit 64
fi

install -d -m 0755 "$webroot/.well-known/acme-challenge"
printf '%s\n' 'vpsbox-webroot-challenge' >"$webroot/.well-known/acme-challenge/vpsbox-fixture"
install -d -m 0755 "/etc/letsencrypt/live/$domain"
openssl req -x509 -nodes -newkey rsa:2048 -days 2 \
  -subj "/CN=$domain" \
  -addext "subjectAltName=DNS:$domain" \
  -keyout "/etc/letsencrypt/live/$domain/privkey.pem" \
  -out "/etc/letsencrypt/live/$domain/fullchain.pem" >/dev/null 2>&1
chmod 0600 "/etc/letsencrypt/live/$domain/privkey.pem"
chmod 0644 "/etc/letsencrypt/live/$domain/fullchain.pem"
printf 'fixture certificate created for %s using webroot %s\n' "$domain" "$webroot"
SH
chmod 0755 /usr/local/bin/certbot

systemctl daemon-reload
systemctl enable --now vpsbox-http-upstream.service
curl --fail --silent --show-error http://127.0.0.1:4101/ready >/dev/null
