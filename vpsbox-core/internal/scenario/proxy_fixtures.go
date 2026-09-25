package scenario

import (
	_ "embed"
	"strings"
)

var (
	//go:embed fixtures/http-proxy/common.sh
	httpProxyCommonFixture string

	//go:embed fixtures/http-proxy/nginx.sh
	httpProxyNginxFixture string

	//go:embed fixtures/http-proxy/caddy.sh
	httpProxyCaddyFixture string
)

func httpProxyFixtureScript(fixture, hostIP string) (string, bool) {
	var (
		variant string
		body    string
	)
	switch fixture {
	case "http-proxy/nginx-default":
		variant = "default"
		body = httpProxyNginxFixture
	case "http-proxy/nginx-foreign":
		variant = "foreign"
		body = httpProxyNginxFixture
	case "http-proxy/caddy-systemd":
		variant = "caddy"
		body = httpProxyCaddyFixture
	default:
		return "", false
	}

	prefix := strings.Join([]string{
		"VPSBOX_FIXTURE_HOST_IP=" + shellLiteral(hostIP),
		"VPSBOX_FIXTURE_VARIANT=" + shellLiteral(variant),
		"",
	}, "\n")
	return prefix + httpProxyCommonFixture + "\n" + body, true
}
