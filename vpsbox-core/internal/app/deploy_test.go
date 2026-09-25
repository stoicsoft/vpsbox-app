package app

import (
	"strings"
	"testing"
)

const sampleLiveAppsOutput = `@@markers
uptime-kuma
wordpress
@@ports
State  Recv-Q Send-Q Local Address:Port  Peer Address:Port Process
LISTEN 0      4096         0.0.0.0:22         0.0.0.0:*
LISTEN 0      4096         0.0.0.0:8086       0.0.0.0:*
LISTEN 0      4096            [::]:8086          [::]:*
LISTEN 0      4096       127.0.0.1:33060      0.0.0.0:*
LISTEN 0      4096         0.0.0.0:9443       0.0.0.0:*
@@containers
wordpress-wordpress-1|0.0.0.0:8086->80/tcp, [::]:8086->80/tcp
wordpress-db-1|3306/tcp, 33060/tcp
coolify-proxy|0.0.0.0:9443->9443/tcp
`

func TestParseLiveApps(t *testing.T) {
	apps := parseLiveApps(sampleLiveAppsOutput, "192.168.2.15")
	if len(apps) != 3 {
		t.Fatalf("expected 3 apps, got %d: %+v", len(apps), apps)
	}

	// Installed templates come first, in catalog order.
	if apps[0].TemplateID != "uptime-kuma" {
		t.Fatalf("expected uptime-kuma first, got %q", apps[0].TemplateID)
	}
	if apps[0].Running {
		t.Fatal("uptime-kuma is not listening, it should not be reported as running")
	}

	wordpress := apps[1]
	if wordpress.TemplateID != "wordpress" {
		t.Fatalf("expected wordpress second, got %q", wordpress.TemplateID)
	}
	if !wordpress.Running {
		t.Fatal("wordpress publishes 8086 and should be running")
	}
	if wordpress.Port != 8086 {
		t.Fatalf("expected port 8086, got %d", wordpress.Port)
	}
	if wordpress.URL != "http://192.168.2.15:8086" {
		t.Fatalf("unexpected url %q", wordpress.URL)
	}
	if wordpress.Container != "wordpress-wordpress-1" {
		t.Fatalf("unexpected container %q", wordpress.Container)
	}

	// A container we did not install still counts as a live app.
	extra := apps[2]
	if extra.TemplateID != "" || extra.Name != "coolify-proxy" || extra.Port != 9443 {
		t.Fatalf("unexpected extra app %+v", extra)
	}
	if !extra.Running {
		t.Fatal("coolify-proxy is listening on 9443")
	}
}

func TestParseLiveAppsSkipsLoopbackAndUnpublishedPorts(t *testing.T) {
	apps := parseLiveApps(sampleLiveAppsOutput, "192.168.2.15")
	for _, app := range apps {
		switch app.Port {
		case 22:
			t.Fatal("ssh should not be reported as an app")
		case 33060, 3306:
			t.Fatalf("loopback/unpublished port %d should be skipped", app.Port)
		}
	}
}

func TestParseLiveAppsEmpty(t *testing.T) {
	if apps := parseLiveApps("@@markers\n@@ports\n@@containers\n", "10.0.0.1"); len(apps) != 0 {
		t.Fatalf("expected no apps, got %+v", apps)
	}
}

func TestAppURL(t *testing.T) {
	cases := []struct {
		host string
		port int
		want string
	}{
		{"10.0.0.1", 8080, "http://10.0.0.1:8080"},
		{"10.0.0.1", 80, "http://10.0.0.1"},
		{"10.0.0.1", 443, "https://10.0.0.1"},
		{"", 8080, ""},
	}
	for _, tc := range cases {
		if got := appURL(tc.host, tc.port); got != tc.want {
			t.Fatalf("appURL(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}

func TestHostPort(t *testing.T) {
	cases := []struct {
		addr string
		port int
		ok   bool
	}{
		{"0.0.0.0:8000", 8000, true},
		{"*:3000", 3000, true},
		{"[::]:80", 80, true},
		{"127.0.0.1:5432", 0, false},
		{"[::1]:11434", 0, false},
		{"Address:Port", 0, false},
		{"0.0.0.0:99999", 0, false},
	}
	for _, tc := range cases {
		port, ok := hostPort(tc.addr)
		if ok != tc.ok || port != tc.port {
			t.Fatalf("hostPort(%q) = (%d, %v), want (%d, %v)", tc.addr, port, ok, tc.port, tc.ok)
		}
	}
}

func TestMarkerScriptRecordsTemplate(t *testing.T) {
	script := markerScript("wordpress")
	if want := "$HOME/.vpsbox/deployed/wordpress"; !strings.Contains(script, want) {
		t.Fatalf("marker script %q does not record %q", script, want)
	}
}
