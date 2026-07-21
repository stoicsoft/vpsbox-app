# Changelog

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
