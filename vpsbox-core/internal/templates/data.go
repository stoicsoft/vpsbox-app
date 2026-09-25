// Package templates holds the curated app templates that `vpsbox deploy` can
// install inside a sandbox. Each template is a self-contained shell script that
// runs over SSH as the default sandbox user (passwordless sudo).
//
// Two shapes live here: whole-VM deploy platforms (Coolify, Dokploy, Dokku,
// CapRover) installed by their own scripts, and single-stack Docker Compose
// apps drawn from the Server Compass catalog. The compose apps each pin a
// distinct host port so several can run on one sandbox without colliding.
package templates

import (
	"fmt"
	"strings"
)

// Category groups templates in the UI and the CLI listing.
type Category string

const (
	CategoryPlatform Category = "platform" // a PaaS you deploy other apps onto
	CategoryApp      Category = "app"      // a single app or tool
)

// Kind describes how a template installs, which sets expectations for how long
// it takes and what it leaves behind.
type Kind string

const (
	KindInstaller Kind = "installer" // vendor script that provisions the whole VM
	KindCompose   Kind = "compose"   // a docker compose stack under ~/vpsbox-apps
	KindStarter   Kind = "starter"   // minimal example, for learning the flow
)

// Template is one installable thing. The map key in Templates is the ID; ID is
// also carried in the struct so List can return self-describing values.
type Template struct {
	ID          string
	Name        string
	Summary     string
	Category    Category
	Kind        Kind
	Icon        string // emoji, mirrors the Server Compass catalog
	Port        int    // host port the dashboard/app is reachable on
	MinMemoryMB int    // recommended floor; the UI nudges a resize below it
	Note        string // credentials or caveats worth surfacing before install
	Install     string
}

// composeInstall assembles a self-contained install script that drops a compose
// file (and an optional .env of generated secrets) under ~/vpsbox-apps/<id> and
// brings the stack up. Re-running is safe: the .env is written only once so
// generated secrets stay stable, and `docker compose up -d` reconciles.
func composeInstall(id string, port int, envScript, compose string) string {
	script := "set -e\n" +
		`APP_DIR="$HOME/vpsbox-apps/` + id + "\"\n" +
		`mkdir -p "$APP_DIR"` + "\n" +
		`cd "$APP_DIR"` + "\n"
	if envScript != "" {
		script += "if [ ! -f .env ]; then\n" + envScript + "fi\n"
	}
	script += "cat > docker-compose.yml <<'YAML'\n" + compose + "YAML\n" +
		"sudo docker compose up -d\n" +
		fmt.Sprintf("sudo ufw allow %d/tcp 2>/dev/null || true\n", port) +
		fmt.Sprintf("echo \"%s is up on :%d\"\n", id, port)
	return script
}

var catalog = []Template{
	// -------------------------------------------------------------------------
	// Deploy platforms — the reason vpsbox exists: try a PaaS on a throwaway VM.
	// -------------------------------------------------------------------------
	{
		ID:          "coolify",
		Name:        "Coolify",
		Summary:     "Self-hosted Heroku/Netlify alternative — deploy apps, databases, and services from a web UI",
		Category:    CategoryPlatform,
		Kind:        KindInstaller,
		Icon:        "🚀",
		Port:        8000,
		MinMemoryMB: 2048,
		Note:        "Pulls several images; give the sandbox 2 GB+ and a few minutes.",
		Install: `set -e
echo "Installing Coolify — downloading images, this can take several minutes…"
curl -fsSL https://cdn.coollabs.io/coolify/install.sh | sudo bash
echo "Coolify is up on :8000 — open it to create your admin account"
`,
	},
	{
		ID:          "dokploy",
		Name:        "Dokploy",
		Summary:     "Open-source PaaS with Docker Swarm, Traefik, and one-click databases",
		Category:    CategoryPlatform,
		Kind:        KindInstaller,
		Icon:        "🅳",
		Port:        3000,
		MinMemoryMB: 2048,
		Note:        "Initialises Docker Swarm and binds ports 80/443/3000; use a fresh sandbox.",
		Install: `set -e
echo "Installing Dokploy — setting up Docker Swarm, this can take several minutes…"
curl -sSL https://dokploy.com/install.sh | sudo sh
echo "Dokploy is up on :3000 — open it to finish setup"
`,
	},
	{
		ID:          "dokku",
		Name:        "Dokku",
		Summary:     "The smallest PaaS — git push to deploy, buildpacks and Dockerfiles",
		Category:    CategoryPlatform,
		Kind:        KindInstaller,
		Icon:        "🐳",
		Port:        80,
		MinMemoryMB: 1024,
		Note:        "After install, finish setup in the browser to add your SSH key.",
		Install: `set -e
DOKKU_VERSION=v0.35.20
echo "Installing Dokku $DOKKU_VERSION — this can take several minutes…"
wget -NP /tmp "https://dokku.com/install/$DOKKU_VERSION/bootstrap.sh"
sudo DOKKU_TAG=$DOKKU_VERSION bash /tmp/bootstrap.sh
echo "Dokku is up on :80 — open http://<host>/ to finish setup"
`,
	},
	{
		ID:          "caprover",
		Name:        "CapRover",
		Summary:     "PaaS with a one-app-per-click marketplace, built on Docker Swarm",
		Category:    CategoryPlatform,
		Kind:        KindInstaller,
		Icon:        "🧢",
		Port:        3000,
		MinMemoryMB: 1024,
		Note:        "Default dashboard password is captain42 — change it on first login.",
		Install: `set -e
echo "Installing CapRover — pulling the image, this can take a few minutes…"
sudo docker rm -f caprover 2>/dev/null || true
sudo docker run -d --restart always --name caprover \
  -p 80:80 -p 443:443 -p 3000:3000 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /captain:/captain \
  caprover/caprover
echo "CapRover is starting on :3000 — default password captain42"
`,
	},

	// -------------------------------------------------------------------------
	// Apps & tools — a curated slice of the Server Compass catalog. Distinct
	// host ports; single-container where possible for a small sandbox.
	// -------------------------------------------------------------------------
	{
		ID:          "uptime-kuma",
		Name:        "Uptime Kuma",
		Summary:     "Self-hosted uptime monitor with a clean status page",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "📈",
		Port:        3001,
		MinMemoryMB: 256,
		Install: composeInstall("uptime-kuma", 3001, "", `services:
  uptime-kuma:
    image: louislam/uptime-kuma:1
    ports:
      - "3001:3001"
    volumes:
      - data:/app/data
    restart: unless-stopped
volumes:
  data:
`),
	},
	{
		ID:          "portainer",
		Name:        "Portainer",
		Summary:     "Web UI to manage Docker containers, images, volumes, and networks",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "🐳",
		Port:        9000,
		MinMemoryMB: 256,
		Install: composeInstall("portainer", 9000, "", `services:
  portainer:
    image: portainer/portainer-ce:latest
    ports:
      - "9000:9000"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - data:/data
    restart: unless-stopped
volumes:
  data:
`),
	},
	{
		ID:          "vaultwarden",
		Name:        "Vaultwarden",
		Summary:     "Lightweight Bitwarden-compatible password manager",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "🔐",
		Port:        8080,
		MinMemoryMB: 128,
		Note:        "Sign-ups are open so you can create the first account; a random admin token is generated.",
		Install: composeInstall("vaultwarden", 8080,
			`printf 'ADMIN_TOKEN=%s\n' "$(openssl rand -hex 32)" > .env
`, `services:
  vaultwarden:
    image: vaultwarden/server:latest
    ports:
      - "8080:80"
    environment:
      - ADMIN_TOKEN=${ADMIN_TOKEN}
      - SIGNUPS_ALLOWED=true
    volumes:
      - data:/data
    restart: unless-stopped
volumes:
  data:
`),
	},
	{
		ID:          "it-tools",
		Name:        "IT-Tools",
		Summary:     "A big box of handy developer utilities in the browser",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "🧰",
		Port:        8081,
		MinMemoryMB: 128,
		Install: composeInstall("it-tools", 8081, "", `services:
  it-tools:
    image: corentinth/it-tools:latest
    ports:
      - "8081:80"
    restart: unless-stopped
`),
	},
	{
		ID:          "dozzle",
		Name:        "Dozzle",
		Summary:     "Real-time log viewer for your Docker containers",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "📋",
		Port:        8082,
		MinMemoryMB: 128,
		Install: composeInstall("dozzle", 8082, "", `services:
  dozzle:
    image: amir20/dozzle:latest
    ports:
      - "8082:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    restart: unless-stopped
`),
	},
	{
		ID:          "gitea",
		Name:        "Gitea",
		Summary:     "Lightweight self-hosted Git service with issues and a wiki",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "🍵",
		Port:        3003,
		MinMemoryMB: 256,
		Note:        "Runs on SQLite for a small footprint; Git SSH is on port 2222.",
		Install: composeInstall("gitea", 3003, "", `services:
  gitea:
    image: gitea/gitea:latest
    ports:
      - "3003:3000"
      - "2222:22"
    environment:
      - USER_UID=1000
      - USER_GID=1000
      - GITEA__database__DB_TYPE=sqlite3
    volumes:
      - data:/data
    restart: unless-stopped
volumes:
  data:
`),
	},
	{
		ID:          "nocodb",
		Name:        "NocoDB",
		Summary:     "Turn any database into a smart spreadsheet — an Airtable alternative",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "📊",
		Port:        8090,
		MinMemoryMB: 256,
		Note:        "Runs on its built-in SQLite store.",
		Install: composeInstall("nocodb", 8090, "", `services:
  nocodb:
    image: nocodb/nocodb:latest
    ports:
      - "8090:8080"
    volumes:
      - data:/usr/app/data
    restart: unless-stopped
volumes:
  data:
`),
	},
	{
		ID:          "n8n",
		Name:        "n8n",
		Summary:     "Workflow automation — connect apps and APIs with a visual editor",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "🔄",
		Port:        5678,
		MinMemoryMB: 512,
		Install: composeInstall("n8n", 5678,
			`printf 'N8N_ENCRYPTION_KEY=%s\n' "$(openssl rand -hex 24)" > .env
`, `services:
  n8n:
    image: docker.n8n.io/n8nio/n8n:latest
    ports:
      - "5678:5678"
    environment:
      - N8N_ENCRYPTION_KEY=${N8N_ENCRYPTION_KEY}
      - N8N_SECURE_COOKIE=false
      - N8N_RUNNERS_ENABLED=true
    volumes:
      - data:/home/node/.n8n
    restart: unless-stopped
volumes:
  data:
`),
	},
	{
		ID:          "memos",
		Name:        "Memos",
		Summary:     "A lightweight, privacy-first note-taking service",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "📝",
		Port:        5230,
		MinMemoryMB: 256,
		Install: composeInstall("memos", 5230, "", `services:
  memos:
    image: neosmemo/memos:stable
    ports:
      - "5230:5230"
    volumes:
      - data:/var/opt/memos
    restart: unless-stopped
volumes:
  data:
`),
	},
	{
		ID:          "pocketbase",
		Name:        "PocketBase",
		Summary:     "Open-source backend in one file — database, auth, and file storage",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "📦",
		Port:        8091,
		MinMemoryMB: 256,
		Note:        "Create the admin account at /_/ on first open.",
		Install: composeInstall("pocketbase", 8091, "", `services:
  pocketbase:
    image: ghcr.io/muchobien/pocketbase:latest
    ports:
      - "8091:8090"
    volumes:
      - data:/pb_data
    restart: unless-stopped
volumes:
  data:
`),
	},
	{
		ID:          "adminer",
		Name:        "Adminer",
		Summary:     "Full-featured database management in a single lightweight tool",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "🗄️",
		Port:        8083,
		MinMemoryMB: 128,
		Install: composeInstall("adminer", 8083, "", `services:
  adminer:
    image: adminer:latest
    ports:
      - "8083:8080"
    restart: unless-stopped
`),
	},
	{
		ID:          "filebrowser",
		Name:        "File Browser",
		Summary:     "A clean web file manager for browsing and sharing files",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "📁",
		Port:        8084,
		MinMemoryMB: 128,
		Note:        "Default login is admin / admin — change it after first sign-in.",
		Install: composeInstall("filebrowser", 8084, "", `services:
  filebrowser:
    image: filebrowser/filebrowser:latest
    ports:
      - "8084:80"
    volumes:
      - files:/srv
      - data:/database
    restart: unless-stopped
volumes:
  files:
  data:
`),
	},
	{
		ID:          "metabase",
		Name:        "Metabase",
		Summary:     "Business intelligence — ask questions of your data and build dashboards",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "📊",
		Port:        3030,
		MinMemoryMB: 1024,
		Note:        "The JVM needs headroom; give the sandbox 2 GB+.",
		Install: composeInstall("metabase", 3030, "", `services:
  metabase:
    image: metabase/metabase:latest
    ports:
      - "3030:3000"
    volumes:
      - data:/metabase-data
    environment:
      - MB_DB_FILE=/metabase-data/metabase.db
    restart: unless-stopped
volumes:
  data:
`),
	},
	{
		ID:          "code-server",
		Name:        "Code Server",
		Summary:     "VS Code in the browser, running on your sandbox",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "💻",
		Port:        8443,
		MinMemoryMB: 1024,
		Note:        "Sign in with the password: vpsbox",
		Install: composeInstall("code-server", 8443, "", `services:
  code-server:
    image: codercom/code-server:latest
    ports:
      - "8443:8080"
    environment:
      - PASSWORD=vpsbox
    volumes:
      - config:/home/coder/.config
      - project:/home/coder/project
    restart: unless-stopped
volumes:
  config:
  project:
`),
	},
	{
		ID:          "stirling-pdf",
		Name:        "Stirling PDF",
		Summary:     "A local toolbox for splitting, merging, and converting PDFs",
		Category:    CategoryApp,
		Kind:        KindCompose,
		Icon:        "📄",
		Port:        8085,
		MinMemoryMB: 1024,
		Install: composeInstall("stirling-pdf", 8085, "", `services:
  stirling-pdf:
    image: stirlingtools/stirling-pdf:latest
    ports:
      - "8085:8080"
    volumes:
      - data:/usr/share/tessdata
    restart: unless-stopped
volumes:
  data:
`),
	},

	// -------------------------------------------------------------------------
	// Starters — minimal examples that show the deploy flow end to end.
	// -------------------------------------------------------------------------
	{
		ID:          "static-html",
		Name:        "Static HTML",
		Summary:     "nginx serving a static welcome page",
		Category:    CategoryApp,
		Kind:        KindStarter,
		Icon:        "📄",
		Port:        80,
		MinMemoryMB: 128,
		Install: `set -e
sudo apt-get update -y
sudo apt-get install -y nginx
sudo tee /var/www/html/index.html >/dev/null <<'HTML'
<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>It works!</title>
<style>body{font-family:sans-serif;text-align:center;padding:10vh;background:#f7f0df;color:#1a1d21}
h1{font-size:48px;margin:0}p{color:#44515f}</style></head>
<body><h1>It works!</h1><p>Served by nginx on your vpsbox sandbox.</p></body></html>
HTML
sudo systemctl enable --now nginx
echo "static-html ready on :80"
`,
	},
	{
		ID:          "nodejs-hello",
		Name:        "Node.js Hello",
		Summary:     "Tiny Node.js HTTP server managed by systemd",
		Category:    CategoryApp,
		Kind:        KindStarter,
		Icon:        "🟢",
		Port:        3000,
		MinMemoryMB: 128,
		Install: `set -e
sudo apt-get update -y
sudo apt-get install -y nodejs
mkdir -p ~/nodejs-hello
cat > ~/nodejs-hello/server.js <<'JS'
const http = require('http');
const server = http.createServer((req, res) => {
  res.writeHead(200, {'Content-Type': 'text/plain'});
  res.end('Hello from your VPS!\n');
});
server.listen(3000, '0.0.0.0', () => console.log('listening on :3000'));
JS
sudo tee /etc/systemd/system/nodejs-hello.service >/dev/null <<'UNIT'
[Unit]
Description=nodejs hello world
After=network.target

[Service]
ExecStart=/usr/bin/node /root/nodejs-hello/server.js
Restart=always
User=root

[Install]
WantedBy=multi-user.target
UNIT
sudo systemctl daemon-reload
sudo systemctl enable --now nodejs-hello
sudo ufw allow 3000/tcp 2>/dev/null || true
echo "nodejs-hello ready on :3000"
`,
	},
	{
		ID:          "wordpress",
		Name:        "WordPress",
		Summary:     "WordPress + MySQL via Docker Compose",
		Category:    CategoryApp,
		Kind:        KindStarter,
		Icon:        "📝",
		Port:        8086,
		MinMemoryMB: 512,
		Install: composeInstall("wordpress", 8086,
			`printf 'DB_PASSWORD=%s\nDB_ROOT_PASSWORD=%s\n' "$(openssl rand -hex 16)" "$(openssl rand -hex 16)" > .env
`, `services:
  db:
    image: mysql:8
    restart: unless-stopped
    environment:
      MYSQL_DATABASE: wp
      MYSQL_USER: wp
      MYSQL_PASSWORD: ${DB_PASSWORD}
      MYSQL_ROOT_PASSWORD: ${DB_ROOT_PASSWORD}
    volumes:
      - db:/var/lib/mysql
  wordpress:
    image: wordpress:latest
    restart: unless-stopped
    depends_on: [db]
    ports:
      - "8086:80"
    environment:
      WORDPRESS_DB_HOST: db
      WORDPRESS_DB_USER: wp
      WORDPRESS_DB_PASSWORD: ${DB_PASSWORD}
      WORDPRESS_DB_NAME: wp
volumes:
  db:
`),
	},
}

// Templates is the ID-keyed lookup Manager.Deploy resolves against.
var Templates = func() map[string]Template {
	m := make(map[string]Template, len(catalog))
	for _, t := range catalog {
		m[t.ID] = t
	}
	return m
}()

// List returns the catalog ordered platforms first, then apps, each group
// alphabetical by name. Starters sort to the end of the app group.
func List() []Template {
	out := make([]Template, len(catalog))
	copy(out, catalog)
	sortCatalog(out)
	return out
}

func sortCatalog(ts []Template) {
	rank := func(t Template) int {
		switch {
		case t.Category == CategoryPlatform:
			return 0
		case t.Kind == KindStarter:
			return 2
		default:
			return 1
		}
	}
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0; j-- {
			a, b := ts[j-1], ts[j]
			nameA, nameB := strings.ToLower(a.Name), strings.ToLower(b.Name)
			if rank(a) < rank(b) || (rank(a) == rank(b) && nameA <= nameB) {
				break
			}
			ts[j-1], ts[j] = ts[j], ts[j-1]
		}
	}
}
