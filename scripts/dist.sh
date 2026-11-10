#!/usr/bin/env bash
# Builds local archives. This script does not publish releases.
# Usage: scripts/dist.sh <version> <commit> <os/arch>...
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ $# -lt 3 ]]; then
  echo "Usage: scripts/dist.sh <version> <commit> <os/arch>..." >&2
  exit 1
fi
version="$1"
commit="$2"
shift 2
if [[ ! "$version" =~ ^[[:alnum:]][[:alnum:].+_-]*$ ]]; then
  echo "Version must contain only letters, digits, dots, plus signs, underscores, and hyphens." >&2
  exit 1
fi
for platform in "$@"; do
  case "$platform" in
    linux/amd64|linux/arm64|darwin/amd64|darwin/arm64) ;;
    *) echo "Unsupported distribution platform: $platform" >&2; exit 1 ;;
  esac
done

buildinfo=github.com/kubebao/openbao-operator-cli/internal/buildinfo
destination="${DIST_DIR:-dist}"
mkdir -p "$destination"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
archives=()
for platform in "$@"; do
  os="${platform%/*}"
  arch="${platform#*/}"
  name="kubebao_${version}_${os}_${arch}"
  mkdir -p "$work/$name"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X ${buildinfo}.Version=${version} -X ${buildinfo}.Commit=${commit}" \
    -o "$work/$name/kubebao" ./cmd/kubebao
  cp LICENSE THIRD_PARTY_LICENSES "$work/$name/"
  tar -C "$work" -czf "$destination/$name.tar.gz" "$name"
  archives+=("$name.tar.gz")
  echo "$destination/$name.tar.gz"
done
(
  cd "$destination"
  shasum -a 256 "${archives[@]}" > checksums.txt
)
echo "$destination/checksums.txt"
