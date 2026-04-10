// Package missions holds the embedded `vpsbox learn` missions — small,
// hands-on exercises with a verify check that runs inside the sandbox.
package missions

type Mission struct {
	ID         string
	Title      string
	Intro      string
	Steps      []string
	Verify     string
	SuccessMsg string
	FailHint   string
}

var Missions = []Mission{
	{
		ID:    "first-shell",
		Title: "Your first shell session",
		Intro: "Open an SSH connection to the sandbox, look around, and find out which user you are. This is the same flow you'll use to log into a real VPS later.",
		Steps: []string{
			"Open a shell:           vpsbox ssh",
			"Print your username:   whoami",
			"Print where you are:   pwd",
			"List files (incl. hidden): ls -la",
			"Exit the shell:        exit",
		},
		Verify:     `whoami | grep -q '^root$' && [ -d /root ] && echo OK`,
		SuccessMsg: "You're now logged in as `root` and you can poke around safely.",
		FailHint:   "Run `vpsbox ssh` and make sure the connection works.",
	},
	{
		ID:    "static-site",
		Title: "Deploy a static website with nginx",
		Intro: "Install nginx and serve a custom homepage. This is the smallest possible 'web deploy'.",
		Steps: []string{
			"Install nginx:         sudo apt-get update && sudo apt-get install -y nginx",
			"Replace the homepage: echo '<h1>Hello from my VPS</h1>' | sudo tee /var/www/html/index.html",
			"Make sure it's running: sudo systemctl enable --now nginx",
			"Open the URL printed by `vpsbox info` in your browser",
		},
		Verify:     `dpkg -s nginx >/dev/null 2>&1 && systemctl is-active --quiet nginx && echo OK`,
		SuccessMsg: "nginx is installed and running. Your sandbox is now serving HTTP.",
		FailHint:   "Install nginx with `sudo apt-get install -y nginx`, then start it with `sudo systemctl start nginx`.",
	},
	{
		ID:    "open-port",
		Title: "Open a port with the firewall",
		Intro: "ufw is Ubuntu's friendly firewall. By default it blocks incoming traffic. You'll allow port 8080.",
		Steps: []string{
			"Allow SSH first (so you don't lock yourself out): sudo ufw allow OpenSSH",
			"Enable the firewall:                              sudo ufw --force enable",
			"Allow port 8080:                                  sudo ufw allow 8080/tcp",
			"Confirm the rule:                                 sudo ufw status",
		},
		Verify:     `sudo ufw status | grep -q '8080/tcp.*ALLOW' && echo OK`,
		SuccessMsg: "Port 8080 is now open. Anything bound to it is reachable from outside the VM.",
		FailHint:   "Run `sudo ufw allow 8080/tcp`. If ufw is inactive, also run `sudo ufw enable`.",
	},
	{
		ID:    "create-user",
		Title: "Create a non-root user with sudo",
		Intro: "Real servers should never let you log in as root. The first thing to do on a new VPS is create a personal user with sudo access.",
		Steps: []string{
			"Create the user:        sudo adduser --disabled-password --gecos '' dev",
			"Give them sudo:         sudo usermod -aG sudo dev",
			"Switch to the user:     sudo -iu dev",
			"Confirm with:           id",
		},
		Verify:     `id dev >/dev/null 2>&1 && id dev | grep -q sudo && echo OK`,
		SuccessMsg: "User `dev` exists and is in the sudo group. On a real server you'd now copy your SSH key to /home/dev/.ssh/authorized_keys.",
		FailHint:   "Run `sudo adduser --disabled-password --gecos '' dev && sudo usermod -aG sudo dev`.",
	},
	{
		ID:    "cron-job",
		Title: "Schedule a recurring task with cron",
		Intro: "Cron is the original task scheduler. You'll write a job that appends the current date to a file every minute.",
		Steps: []string{
			"Add a cron entry: ( crontab -l 2>/dev/null; echo '* * * * * date >> /tmp/cron-test.log' ) | crontab -",
			"Wait one minute.",
			"Check the file: cat /tmp/cron-test.log",
		},
		Verify:     `[ -s /tmp/cron-test.log ] && echo OK`,
		SuccessMsg: "Your cron job is firing every minute and writing to /tmp/cron-test.log.",
		FailHint:   "Add the cron line, then wait at least 60 seconds before re-running verify.",
	},
	{
		ID:    "systemd-service",
		Title: "Write a tiny systemd service",
		Intro: "systemd manages background processes. You'll wrap a one-line shell loop as a service that auto-restarts.",
		Steps: []string{
			"Create the script: sudo tee /usr/local/bin/datelogger.sh >/dev/null <<'EOF'\n#!/bin/sh\nwhile true; do date >> /tmp/svc.log; sleep 5; done\nEOF",
			"Make it executable: sudo chmod +x /usr/local/bin/datelogger.sh",
			"Create the unit file at /etc/systemd/system/datelogger.service with [Service] ExecStart=/usr/local/bin/datelogger.sh, Restart=always",
			"Enable + start: sudo systemctl daemon-reload && sudo systemctl enable --now datelogger",
			"Check status: systemctl status datelogger",
		},
		Verify:     `systemctl is-active --quiet datelogger && echo OK`,
		SuccessMsg: "Your systemd service is running and will auto-restart if it crashes.",
		FailHint:   "Make sure /etc/systemd/system/datelogger.service exists, then `sudo systemctl daemon-reload && sudo systemctl enable --now datelogger`.",
	},
}

func Find(id string) *Mission {
	for i := range Missions {
		if Missions[i].ID == id {
			return &Missions[i]
		}
	}
	return nil
}
