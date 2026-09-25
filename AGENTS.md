# Repository Guidelines

## Project Structure & Module Organization

This repository is the Wails desktop app for VPSBox. Root-level `main.go` bootstraps Wails and `app.go` exposes Go methods to the frontend. `frontend/` contains the React/Vite UI, with source in `frontend/src/`, generated Wails bindings in `frontend/wailsjs/`, and assets under `frontend/src/assets/`. `vpsbox-core/` is the local Go engine module used through the root `go.mod` `replace` directive. Build metadata lives in `build/`, helpers in `scripts/`, and README screenshots in `images/`.

## Build, Test, and Development Commands

- `./scripts/dev.sh`: runs `go mod tidy`, installs frontend dependencies, then starts `wails dev`.
- `./scripts/build.sh`: runs a clean production Wails build and writes artifacts to `build/bin/`; set `VERSION=1.2.3` to stamp a release build.
- `go test ./...`: runs tests for the root desktop module.
- `(cd vpsbox-core && go test ./...)`: runs the core engine test suite in its nested Go module.
- `(cd frontend && npm run build)`: type-checks TypeScript and builds the Vite frontend.

## Coding Style & Naming Conventions

Use `gofmt` for Go files and keep package names short and lowercase. Exported Go types and Wails-bound methods use `PascalCase`; internal helpers use `camelCase`. React components and TypeScript types use `PascalCase`; functions and local state use `camelCase`. The frontend uses 2-space indentation and single quotes. Treat `frontend/wailsjs/` as generated code unless regenerating bindings through Wails.

## Testing Guidelines

Go tests live beside implementation files as `*_test.go`; prefer table-driven tests for core logic. Add or update tests in `vpsbox-core/internal/...` when changing provisioning, backend detection, registry, hosts, or scenario behavior. There is no dedicated JavaScript test runner currently, so validate UI changes with `npm run build` and, when behavior changes, a local `wails dev` smoke test.

## Commit & Pull Request Guidelines

Recent commits use short imperative subjects such as `Fix version detection...` or `Enable Hyper-V...`. Keep subjects concise, describe the user-visible effect, and avoid unrelated cleanup in the same commit. Pull requests should include a brief summary, affected platforms, screenshots or screen recordings for UI changes, linked issues when available, and the exact validation commands run.

## Security & Configuration Tips

Do not commit `node_modules/`, `frontend/dist/`, `build/bin/`, local binaries, private keys, or machine-specific paths. Changes that install packages, alter hosts files, or touch virtualization backends should document expected OS prompts and failure modes.
