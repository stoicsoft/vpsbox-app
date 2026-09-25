# Coolify on vpsbox

1. Boot a sandbox:

```bash
vpsbox up --name coolify
```

2. SSH in and install Coolify with its normal script:

```bash
vpsbox ssh coolify
```

3. Use either:

- `https://coolify.<hyphenated-ip>.sslip.io`
- `https://coolify.vpsbox.local` after adding an explicit app hostname through your reverse proxy

The wildcard-friendly path is the `sslip.io` base stored in `domain_base` inside the instance registry record.
