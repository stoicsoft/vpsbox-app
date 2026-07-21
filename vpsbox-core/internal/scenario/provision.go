package scenario

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

func ProvisionScript(fixture, hostIP string) (string, error) {
	if !allowedFixtures[fixture] {
		return "", fmt.Errorf("unknown fixture %q", fixture)
	}
	if parsed := net.ParseIP(hostIP); parsed == nil || parsed.To4() == nil {
		return "", errors.New("fixture requires a valid IPv4 address")
	}

	wildcardBase := strings.ReplaceAll(hostIP, ".", "-") + ".sslip.io"
	common := `set -euo pipefail
cloud-init status --wait >/dev/null || true
docker info >/dev/null
if [ "$(docker info --format '{{.Swarm.LocalNodeState}}')" = "inactive" ]; then
  docker swarm init --advertise-addr ` + shellLiteral(hostIP) + ` >/dev/null
fi
`

	switch fixture {
	case "base/ubuntu-docker":
		return "set -euo pipefail\ncloud-init status --wait >/dev/null || true\ndocker info >/dev/null\n", nil
	case "generic/swarm-traefik":
		return common + `
docker network inspect generic-public >/dev/null 2>&1 || docker network create --driver overlay generic-public >/dev/null
docker service inspect generic-proxy >/dev/null 2>&1 || docker service create \
  --name generic-proxy \
  --network generic-public \
  --mount type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock,readonly \
  --env DOCKER_API_VERSION=1.40 \
  --publish published=80,target=80 \
  traefik:v3.6.16 \
  --providers.swarm=true \
  --entrypoints.web.address=:80 >/dev/null
if ! docker service inspect generic-proxy --format '{{json .Spec.TaskTemplate.ContainerSpec.Mounts}}' | grep -q '/var/run/docker.sock'; then
  docker service update --mount-add type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock,readonly generic-proxy >/dev/null
fi
if ! docker service inspect generic-proxy --format '{{json .Spec.TaskTemplate.ContainerSpec.Env}}' | grep -q 'DOCKER_API_VERSION=1.40'; then
  docker service update --env-add DOCKER_API_VERSION=1.40 generic-proxy >/dev/null
fi
if ! docker service inspect generic-proxy --format '{{.Spec.TaskTemplate.ContainerSpec.Image}}' | grep -q '^traefik:v3.6.16@'; then
  docker service update --image traefik:v3.6.16 generic-proxy >/dev/null
fi
docker service inspect generic-web >/dev/null 2>&1 || docker service create \
  --name generic-web \
  --network generic-public \
  --label traefik.enable=true \
  --label ` + shellLiteral("traefik.http.routers.generic.rule=Host(`generic."+wildcardBase+"`)") + ` \
  --label traefik.http.services.generic.loadbalancer.server.port=80 \
  nginx:alpine >/dev/null
`, nil
	case "easypanel/mimic", "easypanel/mimic-stable", "easypanel/mimic-canary", "easypanel/mimic-stateful":
		channel := "stable"
		if fixture == "easypanel/mimic-canary" {
			channel = "canary"
		}
		script := common + `
install -d -m 0750 /etc/easypanel/projects/epdemo/web
docker network inspect easypanel >/dev/null 2>&1 || docker network create --driver overlay easypanel >/dev/null
docker service inspect easypanel >/dev/null 2>&1 || docker service create \
  --name easypanel \
  --constraint node.role==manager \
  --label easypanel.control-plane=true \
  --label easypanel.channel=` + shellLiteral(channel) + ` \
  alpine:3.20 sleep infinity >/dev/null
docker service inspect easypanel-proxy >/dev/null 2>&1 || docker service create \
  --name easypanel-proxy \
  --constraint node.role==manager \
  --network easypanel \
  --mount type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock,readonly \
  --env DOCKER_API_VERSION=1.40 \
  --publish published=80,target=80 \
  --publish published=443,target=443 \
  traefik:v3.6.16 \
  --providers.swarm=true \
  --entrypoints.web.address=:80 \
  --entrypoints.websecure.address=:443 >/dev/null
if ! docker service inspect easypanel-proxy --format '{{json .Spec.TaskTemplate.ContainerSpec.Mounts}}' | grep -q '/var/run/docker.sock'; then
  docker service update --mount-add type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock,readonly easypanel-proxy >/dev/null
fi
if ! docker service inspect easypanel-proxy --format '{{json .Spec.TaskTemplate.ContainerSpec.Env}}' | grep -q 'DOCKER_API_VERSION=1.40'; then
  docker service update --env-add DOCKER_API_VERSION=1.40 easypanel-proxy >/dev/null
fi
if ! docker service inspect easypanel-proxy --format '{{.Spec.TaskTemplate.ContainerSpec.Image}}' | grep -q '^traefik:v3.6.16@'; then
  docker service update --image traefik:v3.6.16 easypanel-proxy >/dev/null
fi
docker service inspect epdemo_web >/dev/null 2>&1 || docker service create \
  --name epdemo_web \
  --constraint node.role==manager \
  --network easypanel \
  --env NODE_ENV=production \
  --env LAB_MESSAGE=vpsbox-dummy-data \
  --label easypanel.project=epdemo \
  --label easypanel.service=web \
  --label traefik.enable=true \
  --label ` + shellLiteral("traefik.http.routers.epdemo.rule=Host(`epdemo."+wildcardBase+"`)") + ` \
  --label traefik.http.routers.epdemo.entrypoints=web \
  --label traefik.http.routers.epdemo.service=epdemo-web \
  --label traefik.http.services.epdemo-web.loadbalancer.server.port=80 \
  nginx:alpine >/dev/null
`
		if fixture == "easypanel/mimic-stateful" {
			script += `
install -d -m 0750 /etc/easypanel/projects/epdemo/postgres/data
docker service inspect epdemo_postgres >/dev/null 2>&1 || docker service create \
  --name epdemo_postgres \
  --constraint node.role==manager \
  --network easypanel \
  --env POSTGRES_DB=app \
  --env POSTGRES_USER=app \
  --env POSTGRES_PASSWORD=vpsbox-dummy-password \
  --mount type=bind,source=/etc/easypanel/projects/epdemo/postgres/data,target=/var/lib/postgresql/data \
  --label easypanel.project=epdemo \
  --label easypanel.service=postgres \
  postgres:16-alpine >/dev/null
`
		}
		return script, nil
	default:
		return "", fmt.Errorf("fixture %q has no provisioner", fixture)
	}
}

func shellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
