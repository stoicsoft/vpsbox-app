#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WAILS_BIN="${WAILS_BIN:-$(go env GOPATH)/bin/wails}"

go mod tidy
(cd "${ROOT_DIR}/frontend" && npm install)

exec "${WAILS_BIN}" build -clean
