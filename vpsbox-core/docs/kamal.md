# Kamal on vpsbox

Use `vpsbox` as the remote SSH target for Kamal:

```bash
vpsbox up --name kamal
vpsbox export kamal --format env
```

Then map:

- host: `VPSBOX_HOST`
- user: `VPSBOX_USER`
- key: `VPSBOX_PRIVATE_KEY`

into `config/deploy.yml`.

This is useful for rehearsing Docker, SSH, and proxy setup before pointing Kamal at a real cloud host.
