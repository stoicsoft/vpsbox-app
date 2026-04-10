// Package templates holds the curated app templates that `vpsbox deploy` can
// install inside a sandbox. Each template is a self-contained shell script
// that runs over SSH as the default sandbox user (passwordless sudo).
package templates

type Template struct {
	Name    string
	Summary string
	Install string
	Port    int
}

var Templates = map[string]Template{
	"static-html": {
		Name:    "static-html",
		Summary: "nginx serving a static welcome page (port 80)",
		Port:    80,
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
	"nodejs-hello": {
		Name:    "nodejs-hello",
		Summary: "Tiny Node.js HTTP server managed by systemd (port 3000)",
		Port:    3000,
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
	"wordpress": {
		Name:    "wordpress",
		Summary: "WordPress + MySQL via Docker Compose (port 8080)",
		Port:    8080,
		Install: `set -e
mkdir -p ~/wordpress
cat > ~/wordpress/docker-compose.yml <<'YAML'
services:
  db:
    image: mysql:8
    restart: always
    environment:
      MYSQL_DATABASE: wp
      MYSQL_USER: wp
      MYSQL_PASSWORD: wp
      MYSQL_ROOT_PASSWORD: wproot
    volumes:
      - db:/var/lib/mysql
  wordpress:
    image: wordpress:latest
    restart: always
    depends_on: [db]
    ports:
      - "8080:80"
    environment:
      WORDPRESS_DB_HOST: db
      WORDPRESS_DB_USER: wp
      WORDPRESS_DB_PASSWORD: wp
      WORDPRESS_DB_NAME: wp
volumes:
  db:
YAML
cd ~/wordpress && sudo docker compose up -d
sudo ufw allow 8080/tcp 2>/dev/null || true
echo "wordpress ready on :8080"
`,
	},
}
