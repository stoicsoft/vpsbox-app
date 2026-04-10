# vpsbox Desktop

This folder contains the Wails desktop app for `vpsbox`.

The desktop product goal is deliberately simple:

1. Install the required system packages without teaching users Homebrew
2. Create a local Ubuntu VPS
3. Start, stop, destroy, and shell into that VPS from a native app

## Development

```bash
cd desktop
go mod tidy
cd frontend && npm install && cd ..
$(go env GOPATH)/bin/wails dev
```

## Production build

```bash
cd ..
./scripts/build-desktop.sh
```

That produces a macOS app bundle at:

```text
dist/vpsbox.app
```

## macOS installer package

From the repo root:

```bash
./scripts/package-desktop-macos.sh
```

That creates:

```text
dist/vpsbox-installer.pkg
```
