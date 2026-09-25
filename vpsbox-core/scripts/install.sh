#!/usr/bin/env bash
set -euo pipefail

REPO="${VPSBOX_GITHUB_REPO:-stoicsoft/vpsbox}"
VERSION="${VERSION:-latest}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$arch" in
  x86_64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
esac

if [[ "$VERSION" == "latest" ]]; then
  url="https://github.com/${REPO}/releases/latest/download/vpsbox_${os}_${arch}.tar.gz"
else
  url="https://github.com/${REPO}/releases/download/${VERSION}/vpsbox_${os}_${arch}.tar.gz"
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

curl -fsSL "$url" | tar -xz -C "$tmpdir"
install -m 0755 "$tmpdir/vpsbox" /usr/local/bin/vpsbox
echo "Installed vpsbox to /usr/local/bin/vpsbox"
