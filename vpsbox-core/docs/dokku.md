# Dokku on vpsbox

Dokku is a good fit for the sandbox because it exercises raw Docker, SSH, reverse proxy, and host hardening workflows.

Recommended flow:

```bash
vpsbox up --name dokku
vpsbox ssh dokku
```

Run Dokku's installer inside the guest, then use:

- `dokku.<domain_base>`
- `<app>.<domain_base>`

with the `domain_base` value from `vpsbox info dokku`.
