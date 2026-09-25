package scenario

import (
	"strings"
	"testing"
)

func TestProvisionScriptUsesOnlyBuiltInFixtures(t *testing.T) {
	script, err := ProvisionScript("easypanel/mimic", "192.168.64.10")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "easypanel.project=epdemo") || !strings.Contains(script, "192-168-64-10.sslip.io") {
		t.Fatalf("unexpected mimic script: %s", script)
	}
	if !strings.Contains(script, "cloud-init status --wait >/dev/null || true") || !strings.Contains(script, "docker info >/dev/null") {
		t.Fatalf("fixture readiness must tolerate degraded cloud-init only after checking Docker: %s", script)
	}
	if !strings.Contains(script, "source=/var/run/docker.sock,target=/var/run/docker.sock,readonly") {
		t.Fatalf("EasyPanel mimic proxy must discover Swarm routes through a read-only Docker socket: %s", script)
	}
	if !strings.Contains(script, "DOCKER_API_VERSION=1.40") {
		t.Fatalf("EasyPanel mimic proxy must negotiate with current Docker daemons: %s", script)
	}
	if !strings.Contains(script, "traefik:v3.6.16") {
		t.Fatalf("EasyPanel mimic proxy must pin the Docker API 1.40-compatible Traefik release: %s", script)
	}
	if _, err := ProvisionScript("../../arbitrary.sh", "192.168.64.10"); err == nil {
		t.Fatal("expected arbitrary fixture to fail")
	}
	if _, err := ProvisionScript("easypanel/mimic", "not-an-ip"); err == nil {
		t.Fatal("expected invalid IP to fail")
	}
}

func TestHTTPProxyProvisionScripts(t *testing.T) {
	tests := []struct {
		fixture string
		want    []string
	}{
		{
			fixture: "http-proxy/nginx-default",
			want: []string{
				"VPSBOX_FIXTURE_VARIANT='default'",
				"nginx -T",
				"systemctl restart nginx",
				"listen 443 ssl default_server",
				"vpsbox-upstream-set-port",
				"127.0.0.1",
				"DPkg::Lock::Timeout=120",
				"Acquire::Retries=5",
			},
		},
		{
			fixture: "http-proxy/nginx-foreign",
			want: []string{
				"VPSBOX_FIXTURE_VARIANT='foreign'",
				"VPSBOX_FOREIGN_VHOST_SENTINEL=preserve-unless-explicitly-adopted",
				"foreign-vhost.sha256",
				"sites-enabled/vpsbox-foreign.conf",
			},
		},
		{
			fixture: "http-proxy/caddy-systemd",
			want: []string{
				"VPSBOX_FIXTURE_VARIANT='caddy'",
				"/etc/caddy/Caddyfile",
				"systemctl restart caddy",
				"literal-newline",
				`\nimport /etc/caddy/server-compass/*.caddy`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.fixture, func(t *testing.T) {
			script, err := ProvisionScript(test.fixture, "192.168.64.10")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range test.want {
				if !strings.Contains(script, want) {
					t.Errorf("fixture script does not contain %q", want)
				}
			}
			if !strings.Contains(script, "certbot certonly --webroot") {
				t.Error("fixture must document its webroot-only certbot contract")
			}
			if !strings.Contains(script, "intentionally refuses --nginx") {
				t.Error("fixture certbot stub must refuse the nginx plugin")
			}
		})
	}
}
