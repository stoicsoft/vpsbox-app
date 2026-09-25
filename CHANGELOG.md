# Changelog

# [Unreleased] - Real-VPS Migration, Local Object Storage, and Private Networks

## New Features

- **Push a sandbox onto a real VPS** — `vpsbox push dev-1 --to root@203.0.113.10` replicates the work you did locally onto a real server: the packages you installed, your files under /root, /home, /opt, /srv and /var/www, service configs, docker volume data, enabled services, and running compose projects. The migration plan is shown and confirmed before anything is touched. Machine identity never moves — SSH server settings, host keys, `.ssh` directories, network and firewall configuration all stay put, so a push cannot lock you out of your server.
- **Import a real VPS into a sandbox** — `vpsbox import root@203.0.113.10` clones a real server into a local sandbox sized to hold what runs there. The server is only read from — nothing on it changes. Rehearse the risky upgrade, break the clone, run `vpsbox undo`, and only then touch the real machine.
- **Your machine relays the copy** — the sandbox and the VPS never need to reach each other; every byte flows `ssh → your machine → ssh`. Migrations also survive architecture changes (Apple Silicon sandbox → Intel VPS) because packages are installed from the destination's own apt archive and docker images are pulled on the destination rather than copied.
- **Both flows in the desktop app** — every server has a "Push to VPS" action that previews the full migration plan before the push button appears, and the toolbar's "Import from VPS" checks the server first and shows what it found — its OS, the data to copy, and the sandbox size it will get — before any VM is created. Both run with live stage-by-stage progress and a tail of the real output; closing the window keeps the job running under Activity, and a finished import jumps straight to the new server. SSH keys are picked with a native file dialog, and the last-used server is remembered.

- **Private networks between your servers** — `vpsbox workspace create alpha` allocates a private network, and `vpsbox workspace add alpha db` puts a server on it with a stable address of its own. Members reach each other by name — `psql -h db`, `curl http://api:3000` — instead of by an address that changes every time the server restarts.
- **Workspaces keep groups apart** — Servers on different workspaces cannot reach each other, so you can run a staging group and a production group side by side without them seeing each other. `vpsbox workspace show alpha --check` pings every member from another one and shows you what actually works, rather than only what was configured.
- **Survives a restart** — Private addresses are written into the server's network configuration and the isolation rules are reloaded at boot, so a workspace is still there after you stop and start a server. `vpsbox workspace sync` repairs any member that was switched off while things changed.

- **S3 buckets on your own machine** — `vpsbox bucket create backups` gives you a real S3-compatible endpoint locally, no cloud account and no bill. Buckets live on your machine rather than inside a sandbox, so they outlive the server that wrote to them — which is what finally makes "back up the database, wipe the server, restore it" something you can rehearse before doing it for real.
- **Sandboxes come pre-wired** — `vpsbox bucket attach` teaches a sandbox how to reach the store and drops credentials where every S3 client already looks for them, so nothing inside the box needs a flag or a config file: `pg_dump mydb | s3 cp - s3://backups/mydb.sql` just works. Both path-style and `bucket.buckets.vpsbox.local` subdomain addressing resolve, so SDKs that insist on either one behave.
- **Shared across sandboxes** — Every sandbox talks to the same store, so backing up from one box and restoring into another is a thing you can practise too.
- **Careful with the one thing snapshots can't undo** — Deleting a bucket that still holds objects is refused unless you pass `--force`. Stopping the store keeps your buckets and their contents on disk, and reuses the same credentials on the next start so already-attached sandboxes keep working.

# [1.5.0] - In-App Updates

## New Features

- **Update without leaving the app** — When a new version is available, VPSBox now downloads it for you and installs it on restart. The banner and the Host environment screen show download progress, and a single "Restart and install" button finishes the job — no quitting, downloading, and dragging the app over the old one by hand. Downloads can be cancelled, and the Help menu's update check offers the same one-click install.
- **Careful about what it installs** — Every download is checked against a checksum list signed by the VPSBox release pipeline, so a build that didn't come from it is refused on every platform. The download is also matched to your exact platform and processor, checked for the right architecture before anything is replaced, and on macOS must carry a Developer ID signature from the same developer as the copy you're running. If VPSBox can't safely replace itself — running from a disk image, from a read-only location, or from a folder your account can't write to — it says why and links to the manual download instead.
- **A failed update leaves a working app** — The installed version is moved aside rather than deleted, and put straight back if anything goes wrong, so an interrupted update can't leave you without an app. The details of every install are written to `~/.vpsbox/logs/update.log`.

# [1.4.0] - One-Click Deploy and App Catalog

## New Features

- **Deploy platforms and apps from the desktop** — Every server has a new Deploy tab. Install a self-hosted deploy platform — Coolify, Dokploy, Dokku, or CapRover — onto your sandbox in one click, or pick from a curated set of ready-to-run apps and tools including Gitea, n8n, Uptime Kuma, Portainer, NocoDB, Vaultwarden, Metabase, Code Server, and more. Each install runs as a tracked job you can watch from the status bar, and once it's up, an Open button takes you straight to it.
- **Right-sized for your sandbox** — Templates that need more memory than the sandbox currently has are flagged before you install, so you can resize first instead of watching an install run out of room. Passwords and encryption keys are generated on the server itself, never written into anything shipped to it.
- **The full catalog with Server Compass** — The Deploy tab links straight to Server Compass, which deploys the complete catalog of 400+ apps, databases, and stacks to real VPS fleets when you outgrow the sandbox.

## Improvements

- **Group the command-line deploy list** — `vpsbox deploy --list` now separates deploy platforms from apps and tools, and reads in the same order as the desktop.
- **Keep checking for updates while the app is open** — VPSBox re-checks for a newer release on a schedule, so a long-running window surfaces an update in the banner without needing a restart.
- **Faster failure when deploying to a stopped sandbox** — Installing now confirms the sandbox is running and tells you to start it first, instead of waiting on a stopped VM.

# [1.3.0] - Snapshots and Change Tracking

## New Features

- **Save and restore snapshots from the desktop** — Every server has a new Snapshots tab. Save a named checkpoint of a running sandbox, see your saved points listed newest first, and roll back to any of them — or undo to the most recent — in a click. Deleting a snapshot you no longer need reclaims its disk space.
- **See what changed since your checkpoint** — The Snapshots tab tracks how a sandbox has drifted from its last checkpoint, listing the packages, services, listening ports, and files that were added, removed, or modified. Filter the changes by type or search them to find a specific one.
- **Native macOS menu bar** — VPSBox now has a proper menu bar with standard Edit and Window commands, an About VPSBox panel showing your installed version, a Help menu that checks for updates, and quick links to the website and release notes.

## Improvements

- **Delete snapshots from the command line** — `vpsbox unsnapshot --snapshot <name>` removes a saved snapshot and reclaims its space, and `vpsbox reset` now reports which snapshot it restored from.
- **Faster, clearer failures when a sandbox is stopped** — Saving a checkpoint or reading changes now confirms the sandbox is running and tells you to start it first, instead of waiting on a stopped VM.

## Bug Fixes

- Fixed **change tracking reporting phantom changes after restoring an older snapshot** — Restoring now re-reads the sandbox and re-anchors the comparison baseline to the point you rolled back to, so the next comparison measures from there rather than from the newest checkpoint.
- Fixed **the comparison baseline outliving the snapshot it described** — Deleting the snapshot a baseline was measured against now clears that baseline too, so change tracking never compares against a point the sandbox can no longer be restored to.

# [1.2.0] - Native Redesign and Whole-App Zoom

## New Features

- **A redesigned, native interface** — VPSBox now reads like a Mac developer tool. A unified toolbar doubles as the title bar, your servers live in a source list, connection details are laid out as inspector rows, and a status bar keeps server counts, the VM backend, and any running work in view.
- **Dark mode** — The whole app follows your system appearance and switches between light and dark automatically.
- **Scale the entire app** — Zoom from 80% to 160% with ⌘+ and ⌘− (Ctrl on Windows and Linux), reset with ⌘0, or use the stepper at the right of the status bar. Text, icons, tables, and spacing scale together, and your setting is remembered the next time you open VPSBox.

## Improvements

- **Read logs like a traffic list** — Entries are color-coded by category (system, connections, routes, Docker), warnings and errors are flagged in the margin, and the table header stays put while you scroll.
- **See server state at a glance** — Every server in the sidebar carries a status light: green while running, amber and pulsing while it starts, grey when stopped.
- **Scan connection details faster** — Hostnames, IP addresses, SSH users, and key paths are set in a monospace face and aligned in columns, so addresses and paths are easier to read, compare, and select.
- **Keep your place while you work** — Creating, resizing, and deleting a server now open as sheets that drop from the toolbar, leaving the server you were looking at visible behind them.
- **Clearer status wording** — Host packages and local hostnames report "Ready", "Configured", or "2 of 3" instead of borrowing server lifecycle words like "Running".

## Bug Fixes

- Fixed **dialogs not closing from the keyboard** — Pressing Escape now dismisses any open sheet.

# [1.1.0] - Labs and Live Diagnostics

## New Features

- **Create reproducible migration labs** — Use validated, versioned manifests to create, resume, inspect, snapshot, reset, export, and safely destroy multi-VPS test environments. Included EasyPanel smoke, compatibility, and full profiles use synthetic fixtures.
- **Inspect live server diagnostics** — Open the desktop Logs tab to monitor system journal messages, network connections, routes, Docker containers, container output, and recent Docker activity with automatic refresh, category filters, and text search.
- **Check for VPSBox updates** — View the installed and latest versions in the System screen, run a fresh update check, see connection or release errors, and open the download when a newer build is available.

## Improvements

- **Export complete sandbox connections** — Server Compass and shell exports now include wildcard domain details, TLS certificate paths, and migration-lab roles so sandboxes are easier to import, identify, and access through their generated domains.

## Bug Fixes

- Fixed **Multipass checkpoint restores failing in non-interactive jobs** — Restores can now discard the current VM state without waiting for terminal confirmation.
- Fixed **successful stops and destroys being reported as failed** — A missing privilege for updating local domains no longer overrides a completed lifecycle action; users can apply the pending domains separately.
- Fixed **destroyed sandboxes leaving local artifacts behind** — Key pairs, TLS certificates, cloud-init data, and checkpoint baselines are now removed with the sandbox.
- Fixed **Multipass provisioning scripts losing executable permissions** — Cloud-init now preserves the script permission format when Multipass merges vendor configuration.
