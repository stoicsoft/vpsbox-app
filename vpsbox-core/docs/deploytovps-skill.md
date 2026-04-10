# deploytovps Skill

The skill-facing contract is the same as Server Compass:

1. Discover `~/.vpsbox/instances.json`.
2. Pick the running instance the user wants.
3. SSH as `ubuntu` with the generated key.

Recommended bootstrap flow:

```bash
vpsbox up
vpsbox export dev-1 --format env
```

Then map:

- `VPSBOX_HOST`
- `VPSBOX_PORT`
- `VPSBOX_USER`
- `VPSBOX_PRIVATE_KEY`

into whatever variables the skill expects for a real VPS deployment.
