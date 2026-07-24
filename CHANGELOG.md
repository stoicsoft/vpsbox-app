# Changelog

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
