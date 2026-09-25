# Dokploy on vpsbox

Dokploy can be tested in the sandbox exactly like a low-cost VPS:

```bash
vpsbox up --name dokploy
vpsbox ssh dokploy
```

Inside the guest, run Dokploy's regular installer. Use the instance `domain_base` from `vpsbox info dokploy` for wildcard-compatible routing.
