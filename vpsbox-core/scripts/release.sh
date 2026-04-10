#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-dev}"
OUT_DIR="${OUT_DIR:-dist}"
mkdir -p "$OUT_DIR"

targets=(
  "darwin arm64"
  "darwin amd64"
  "linux amd64"
  "linux arm64"
  "windows amd64"
)

for target in "${targets[@]}"; do
  read -r goos goarch <<<"$target"
  bin_name="vpsbox"
  if [[ "$goos" == "windows" ]]; then
    bin_name="vpsbox.exe"
  fi

  workdir="$(mktemp -d)"
  GOOS="$goos" GOARCH="$goarch" go build \
    -ldflags="-s -w -X github.com/stoicsoft/vpsbox/internal/app.Version=${VERSION}" \
    -o "${workdir}/${bin_name}" \
    ./cmd/vpsbox

  tarball="${OUT_DIR}/vpsbox_${goos}_${goarch}.tar.gz"
  tar -C "$workdir" -czf "$tarball" "$bin_name"
  rm -rf "$workdir"
  echo "built ${tarball}"
done
