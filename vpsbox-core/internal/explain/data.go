// Package explain holds the friendly knowledge base used by `vpsbox explain`.
// Entries are aimed at first-time VPS users who hit a command in a tutorial
// and want to know "what would this do to my server?" before they run it.
package explain

type Entry struct {
	Command    string
	Summary    string
	WhatItDoes string
	Pitfalls   string
	Examples   []string
}

var Entries = map[string]Entry{
	"chmod": {
		Command:    "chmod",
		Summary:    "Change file permissions (read / write / execute for owner, group, everyone).",
		WhatItDoes: "Modifies the access bits on files and directories. The numeric mode like 755 means owner=rwx, group=r-x, world=r-x. Symbolic mode like +x adds the execute bit.",
		Pitfalls:   "`chmod -R 777 /var/www` makes everything world-writable — never do this on a real server. Standard safe values: 644 for files, 755 for directories, 600 for SSH private keys.",
		Examples: []string{
			"chmod 755 script.sh           # owner full, others read+execute",
			"chmod 600 ~/.ssh/id_ed25519   # private key — owner only",
			"chmod +x script.sh            # add the execute bit",
		},
	},
	"chown": {
		Command:    "chown",
		Summary:    "Change which user / group owns a file.",
		WhatItDoes: "Reassigns the owner (and optionally group) of a file or directory. Many install scripts use it to give a service its own data directory.",
		Pitfalls:   "Running `chown -R` on the wrong path can lock you out of system directories. Always double-check the path.",
		Examples: []string{
			"sudo chown ubuntu:ubuntu /opt/myapp",
			"sudo chown -R www-data:www-data /var/www/html",
		},
	},
	"rm": {
		Command:    "rm",
		Summary:    "Delete files. There is NO undo.",
		WhatItDoes: "Removes the file from the filesystem. -r recursively deletes directories. -f skips the confirmation prompt.",
		Pitfalls:   "`rm -rf /` deletes the entire system. `rm -rf $VAR/` where $VAR is empty becomes `rm -rf /`. Always echo the path first if it's built from variables. On vpsbox you can `vpsbox panic` to roll back, but on a real server this is permanent.",
		Examples: []string{
			"rm file.txt              # delete one file",
			"rm -r old-folder         # delete a directory recursively",
			"rm -i *.log              # prompt before each delete",
		},
	},
	"sudo": {
		Command:    "sudo",
		Summary:    "Run a command as the root user (or another user) with elevated permissions.",
		WhatItDoes: "Temporarily grants admin powers for a single command. Anything in /etc, /usr, /var, or system services usually needs sudo.",
		Pitfalls:   "`sudo` does not protect you — it gives you the rope. Read the command before pressing Enter. Avoid `sudo su -` unless you know why you need a root shell.",
		Examples: []string{
			"sudo apt-get update",
			"sudo systemctl restart nginx",
			"sudo -iu dev            # become the 'dev' user with their environment",
		},
	},
	"systemctl": {
		Command:    "systemctl",
		Summary:    "Control systemd services (the standard way to start / stop background processes on Ubuntu).",
		WhatItDoes: "Starts, stops, restarts, enables (auto-start at boot), disables, and inspects services. Most installed apps register a unit file under /etc/systemd/system/.",
		Pitfalls:   "`enable` and `start` are different. `enable` makes a service start at boot but does NOT start it now — use `enable --now` to do both.",
		Examples: []string{
			"sudo systemctl status nginx",
			"sudo systemctl restart nginx",
			"sudo systemctl enable --now nginx     # start now AND on every boot",
			"systemctl list-units --type=service --state=running",
		},
	},
	"ufw": {
		Command:    "ufw",
		Summary:    "Uncomplicated Firewall — Ubuntu's friendly front-end for iptables.",
		WhatItDoes: "Allows or blocks incoming connections by port. Off by default — enable it once, then add `allow` rules per service.",
		Pitfalls:   "If you enable ufw without first allowing OpenSSH, you will lock yourself out of a real VPS. Always run `sudo ufw allow OpenSSH` BEFORE `sudo ufw enable`.",
		Examples: []string{
			"sudo ufw allow OpenSSH      # do this first",
			"sudo ufw allow 80/tcp",
			"sudo ufw enable",
			"sudo ufw status numbered",
		},
	},
	"apt": {
		Command:    "apt",
		Summary:    "Install, remove, and upgrade software packages on Ubuntu / Debian.",
		WhatItDoes: "Talks to the package manager. `update` refreshes the package lists, `upgrade` installs newer versions, `install` adds new packages.",
		Pitfalls:   "Always run `sudo apt-get update` before `install` or you'll get 404s when packages have moved. `apt` and `apt-get` are nearly identical — use `apt-get` in scripts (more stable output).",
		Examples: []string{
			"sudo apt-get update",
			"sudo apt-get install -y nginx",
			"sudo apt-get remove --purge old-package",
			"apt list --installed | wc -l",
		},
	},
	"ssh": {
		Command:    "ssh",
		Summary:    "Open a secure shell session to a remote machine.",
		WhatItDoes: "Logs into a remote computer over an encrypted connection. Authenticates with a password or (better) an SSH key.",
		Pitfalls:   "Never share your private key (id_ed25519, id_rsa). The .pub file is the public half — that's what goes on servers.",
		Examples: []string{
			"ssh ubuntu@1.2.3.4",
			"ssh -i ~/.ssh/mykey ubuntu@host",
			"ssh user@host 'uptime'      # run one command and exit",
		},
	},
	"scp": {
		Command:    "scp",
		Summary:    "Copy files to or from a remote machine over SSH.",
		WhatItDoes: "Like `cp` but one of the paths can live on a remote host. Uses the same auth (key or password) as `ssh`.",
		Pitfalls:   "Big trees can be slow — for many files prefer `rsync -av --progress`.",
		Examples: []string{
			"scp app.zip ubuntu@host:/tmp/",
			"scp ubuntu@host:/var/log/syslog ./",
			"scp -r ./site ubuntu@host:/var/www/",
		},
	},
	"ls": {
		Command:    "ls",
		Summary:    "List the contents of a directory.",
		WhatItDoes: "Shows files and folders. -l adds details (permissions, size, date). -a includes hidden files (starting with .). -h shows sizes in KB/MB/GB.",
		Pitfalls:   "On Linux, files starting with a dot are hidden by default. Use `ls -la` to see them.",
		Examples: []string{
			"ls",
			"ls -la /etc",
			"ls -lh /var/log",
		},
	},
	"ps": {
		Command:    "ps",
		Summary:    "Show running processes.",
		WhatItDoes: "Snapshot of every process at the moment you ran it. The most useful invocation is `ps aux` (every process by every user, with details).",
		Pitfalls:   "ps shows a snapshot, not live updates. For live, use `top` or `htop`.",
		Examples: []string{
			"ps aux",
			"ps aux | grep nginx",
			"ps -eo pid,user,cmd --sort=-%mem | head",
		},
	},
	"kill": {
		Command:    "kill",
		Summary:    "Send a signal to a process — usually to stop it.",
		WhatItDoes: "By default sends SIGTERM (15), asking the process to shut down nicely. -9 sends SIGKILL, which the kernel forces immediately and the process can't clean up.",
		Pitfalls:   "Reach for plain `kill` first. `kill -9` is a last resort — it can leave files corrupted because the process never got to flush them.",
		Examples: []string{
			"kill 1234           # ask process 1234 to exit",
			"kill -9 1234        # force-kill (only if it's stuck)",
			"pkill nginx         # kill by name",
		},
	},
	"journalctl": {
		Command:    "journalctl",
		Summary:    "Read systemd's unified log (the 'journal').",
		WhatItDoes: "Shows logs from every systemd-managed service in one place. -u filters by unit, -f tails live, -n N shows the last N lines.",
		Pitfalls:   "`journalctl` without arguments dumps everything since boot — pipe to `less` or use -n.",
		Examples: []string{
			"sudo journalctl -u nginx -n 100",
			"sudo journalctl -f",
			"sudo journalctl --since '10 min ago'",
		},
	},
	"df": {
		Command:    "df",
		Summary:    "Show how much disk space is used and free.",
		WhatItDoes: "Reports usage per filesystem. -h makes the output human-readable (GB instead of 1024-byte blocks).",
		Pitfalls:   "df shows mounted filesystems. If you're running out of space, also check `du -sh /var/log/*` for log file bloat and `docker system df` if you use Docker.",
		Examples: []string{
			"df -h",
			"df -h /",
			"df -i             # inodes (file count limit), not bytes",
		},
	},
	"free": {
		Command:    "free",
		Summary:    "Show how much memory is used and free.",
		WhatItDoes: "Reports total / used / free / cache. -h makes it human-readable.",
		Pitfalls:   "Linux uses spare RAM as a disk cache, so 'used' usually looks high. The number that matters is 'available' — that's how much you can actually allocate.",
		Examples: []string{
			"free -h",
			"free -m         # in MiB",
		},
	},
	"top": {
		Command:    "top",
		Summary:    "Live view of running processes, sorted by CPU usage.",
		WhatItDoes: "Updates every few seconds. Press q to quit, M to sort by memory, P by CPU.",
		Pitfalls:   "If you have it, `htop` is much friendlier — install it with `sudo apt-get install -y htop`.",
		Examples: []string{
			"top",
			"top -o %MEM     # sort by memory",
		},
	},
	"curl": {
		Command:    "curl",
		Summary:    "Make HTTP requests from the command line.",
		WhatItDoes: "Fetches a URL and prints the response. Supports GET, POST, headers, file uploads, and many protocols beyond HTTP.",
		Pitfalls:   "`curl | sudo bash` runs untrusted code as root. Always inspect the script first.",
		Examples: []string{
			"curl https://example.com",
			"curl -I https://example.com    # headers only",
			"curl -X POST -d '{\"a\":1}' -H 'content-type: application/json' http://localhost:3000/api",
		},
	},
	"tail": {
		Command:    "tail",
		Summary:    "Show the last lines of a file (or follow it live).",
		WhatItDoes: "By default prints the last 10 lines. -n changes the count, -f follows new lines as they arrive (Ctrl-C to stop).",
		Pitfalls:   "Use -F (capital) instead of -f when tailing log files that get rotated — -F reopens the file by name.",
		Examples: []string{
			"tail -n 50 /var/log/syslog",
			"sudo tail -F /var/log/nginx/access.log",
		},
	},
	"grep": {
		Command:    "grep",
		Summary:    "Search for text inside files (or piped input).",
		WhatItDoes: "Prints lines that match a pattern. -i is case-insensitive, -r recurses into directories, -n shows line numbers, -v inverts (show non-matches).",
		Pitfalls:   "By default grep treats the pattern as a regex. Use -F for plain strings if your search has special chars like . or *.",
		Examples: []string{
			"grep error /var/log/syslog",
			"grep -rn TODO ./src",
			"ps aux | grep nginx | grep -v grep",
		},
	},
	"find": {
		Command:    "find",
		Summary:    "Search for files by name, size, modification time, etc.",
		WhatItDoes: "Walks a directory tree and prints (or acts on) matching paths. Powerful and quirky — the path comes BEFORE the filters.",
		Pitfalls:   "`-exec rm {}` is a foot-gun on a typo. Always run the find first to see what would be deleted, THEN add -exec.",
		Examples: []string{
			"find /var/log -name '*.gz'",
			"find . -type f -size +100M",
			"find /tmp -mtime +7 -type f",
		},
	},
}
