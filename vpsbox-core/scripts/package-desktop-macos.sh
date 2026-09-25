#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-0.1.0-dev}"
DIST_DIR="${ROOT_DIR}/dist"
APP_PATH="${DIST_DIR}/vpsbox.app"
PKG_PATH="${DIST_DIR}/vpsbox-installer.pkg"

"${ROOT_DIR}/scripts/build-desktop.sh"

mkdir -p "${DIST_DIR}"
rm -f "${PKG_PATH}"

pkgbuild \
  --identifier com.servercompass.vpsbox \
  --version "${VERSION}" \
  --install-location /Applications \
  --component "${APP_PATH}" \
  "${PKG_PATH}"

echo "Created installer package at ${PKG_PATH}"
