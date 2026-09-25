vpsbox_apt install -y --no-install-recommends nginx ssl-cert

wildcard_base="${VPSBOX_FIXTURE_HOST_IP//./-}.sslip.io"
foreign_domain="foreign.${wildcard_base}"
printf 'VPSBOX_FIXTURE_FOREIGN_DOMAIN=%s\n' "$foreign_domain" >/etc/vpsbox-http-proxy/nginx.env

install -d -m 0755 /var/www/html
cat >/var/www/html/index.html <<'EOF'
vpsbox nginx packaged-default-path fixture
EOF

cat >/etc/nginx/sites-available/default <<'EOF'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    listen 443 ssl default_server;
    listen [::]:443 ssl default_server;

    server_name _;
    root /var/www/html;
    index index.html;

    ssl_certificate /etc/ssl/certs/ssl-cert-snakeoil.pem;
    ssl_certificate_key /etc/ssl/private/ssl-cert-snakeoil.key;

    location / {
        try_files $uri $uri/ =404;
    }
}
EOF

cat >/etc/nginx/sites-available/vpsbox-foreign.conf <<EOF
# VPSBOX_FOREIGN_VHOST_SENTINEL=preserve-unless-explicitly-adopted
server {
    listen 80;
    listen [::]:80;
    listen 443 ssl;
    listen [::]:443 ssl;

    server_name ${foreign_domain};
    ssl_certificate /etc/ssl/certs/ssl-cert-snakeoil.pem;
    ssl_certificate_key /etc/ssl/private/ssl-cert-snakeoil.key;

    location / {
        default_type text/plain;
        return 200 "vpsbox foreign vhost sentinel\n";
    }
}
EOF
sha256sum /etc/nginx/sites-available/vpsbox-foreign.conf |
  awk '{print $1}' >/etc/vpsbox-http-proxy/foreign-vhost.sha256

cat >/usr/local/bin/vpsbox-nginx-set-state <<'SH'
#!/usr/bin/env bash
set -euo pipefail

state="${1:-}"
case "$state" in
  default)
    ln -sfn /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default
    rm -f /etc/nginx/sites-enabled/vpsbox-foreign.conf
    ;;
  foreign)
    ln -sfn /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default
    ln -sfn /etc/nginx/sites-available/vpsbox-foreign.conf /etc/nginx/sites-enabled/vpsbox-foreign.conf
    ;;
  *)
    echo "usage: vpsbox-nginx-set-state default|foreign" >&2
    exit 64
    ;;
esac

nginx -t
systemctl enable nginx >/dev/null
systemctl restart nginx
printf '%s\n' "$state" >/etc/vpsbox-http-proxy/nginx-state
SH
chmod 0755 /usr/local/bin/vpsbox-nginx-set-state

cat >/usr/local/bin/vpsbox-nginx-fixture-check <<'SH'
#!/usr/bin/env bash
set -euo pipefail

. /etc/vpsbox-http-proxy/nginx.env
state="$(cat /etc/vpsbox-http-proxy/nginx-state)"
nginx -t
nginx -T >/tmp/vpsbox-nginx-inventory.txt 2>&1
systemctl is-active --quiet nginx
ss -H -ltn 'sport = :80' | grep -q .
ss -H -ltn 'sport = :443' | grep -q .
curl --fail --silent --show-error http://127.0.0.1:4101/fixture-check |
  grep -q 'port=4101'

if [[ "$state" == "default" ]]; then
  test ! -e /etc/nginx/sites-enabled/vpsbox-foreign.conf
else
  curl --fail --silent --show-error \
    --resolve "${VPSBOX_FIXTURE_FOREIGN_DOMAIN}:80:127.0.0.1" \
    "http://${VPSBOX_FIXTURE_FOREIGN_DOMAIN}/" |
    grep -q 'vpsbox foreign vhost sentinel'
  expected="$(cat /etc/vpsbox-http-proxy/foreign-vhost.sha256)"
  actual="$(sha256sum /etc/nginx/sites-available/vpsbox-foreign.conf | awk '{print $1}')"
  [[ "$actual" == "$expected" ]]
fi

printf 'nginx fixture %s is ready; inventory: /tmp/vpsbox-nginx-inventory.txt\n' "$state"
SH
chmod 0755 /usr/local/bin/vpsbox-nginx-fixture-check

vpsbox-nginx-set-state "$VPSBOX_FIXTURE_VARIANT"
vpsbox-nginx-fixture-check
