#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WAILS_BIN="${WAILS_BIN:-$(go env GOPATH)/bin/wails}"
DIST_DIR="${ROOT_DIR}/dist"
OUTPUT_APP="${DIST_DIR}/vpsbox.app"
TMP_DIR="$(mktemp -d)"
TMP_REPO="${TMP_DIR}/repo"

cleanup() {
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

mkdir -p "${DIST_DIR}"
rm -rf "${OUTPUT_APP}"

rsync -a \
  --exclude .git \
  --exclude dist \
  --exclude desktop/build/bin \
  --exclude desktop/frontend/node_modules \
  --exclude desktop/frontend/dist \
  "${ROOT_DIR}/" "${TMP_REPO}/"

cd "${TMP_REPO}/desktop"
go mod tidy
(cd frontend && npm install)
"${WAILS_BIN}" build -v 1

cp -R "${TMP_REPO}/desktop/build/bin/vpsbox.app" "${OUTPUT_APP}"
echo "Built desktop app at ${OUTPUT_APP}"
