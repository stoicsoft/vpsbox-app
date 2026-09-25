#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WAILS_BIN="${WAILS_BIN:-$(go env GOPATH)/bin/wails}"
VERSION="${VERSION:-dev}"

go mod tidy
(cd "${ROOT_DIR}/frontend" && npm install)

exec "${WAILS_BIN}" build -clean -ldflags "-X 'github.com/stoicsoft/vpsbox/internal/app.Version=${VERSION}'"
