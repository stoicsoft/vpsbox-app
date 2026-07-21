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
