#!/usr/bin/env bash
# Download checksum-pinned release tools without changing the host environment.
set -euo pipefail
project_dir="$(cd "$(dirname "$0")/.." && pwd)"
tool="${1:?Usage: scripts/release-tool.sh <goreleaser|syft|krew> [args...]}"
shift
case "$(uname -s)/$(uname -m)" in
  Darwin/arm64) platform=darwin_arm64 ;;
  Darwin/x86_64) platform=darwin_amd64 ;;
  Linux/aarch64|Linux/arm64) platform=linux_arm64 ;;
  Linux/x86_64) platform=linux_amd64 ;;
  *) echo "Unsupported release-tool host platform." >&2; exit 1 ;;
esac
member="$tool"
case "$tool" in
  goreleaser)
    version=2.20.2
    repository=goreleaser/goreleaser
    case "$platform" in
      darwin_arm64) asset=goreleaser_Darwin_arm64.tar.gz; checksum=4e68482f6bbdb2d3ce7f835be50a4eb7287ee999cefc563c49c5f42ad3778c78 ;;
      darwin_amd64) asset=goreleaser_Darwin_x86_64.tar.gz; checksum=58814536817f6ad787607ed7dfc9e774b3f1334dcffc87002ae2a54fd7b5c5c1 ;;
      linux_arm64) asset=goreleaser_Linux_arm64.tar.gz; checksum=f0fa989c3531348e8f975aac647eb187a09046f8a34c54e0b79bed66c84b7f96 ;;
      linux_amd64) asset=goreleaser_Linux_x86_64.tar.gz; checksum=f3d24969a7389a46613875711d15a96d8f6ef1553d84c6ace09d69ac1e7d44c0 ;;
    esac
    ;;
  krew)
    version=0.5.0
    repository=kubernetes-sigs/krew
    asset="krew-${platform}.tar.gz"
    member="./krew-${platform}"
    case "$platform" in
      darwin_arm64) checksum=cd6e58b4e954e301abd19001d772846997216d696bcaa58f0bcf04708339ece3 ;;
      darwin_amd64) checksum=2d60559126452b57e3df0612f0475a473363f064da35f817290dbbcd877d1ea8 ;;
      linux_arm64) checksum=ab7a98b992424e76b6c162f8b67fb76c4b1e243598aa2807bdf226752f964548 ;;
      linux_amd64) checksum=5d5a221fffdf331d1c5c68d9917530ecd102e0def5b5a6d62eeed1c404efb28a ;;
    esac
    ;;
  syft)
    version=1.57.1
    repository=anchore/syft
    asset="syft_${version}_${platform}.tar.gz"
    case "$platform" in
      darwin_arm64) checksum=e0a74a5d2688d1254b4e17b71f068a83215e3625f846265bf6614c7ba06f3a96 ;;
      darwin_amd64) checksum=2355758c1142982a67b309604c5d75effdbe638a57c771979ea8fd1a16656abc ;;
      linux_arm64) checksum=aa3d3e07344f590bcce8d31043b752daa76c8a56c2aa34ceeee63c6168152582 ;;
      linux_amd64) checksum=bf52519eb57f675d47c07d43213cb6dff6449f3d6d1147ff6701ed91f44eef0c ;;
    esac
    ;;
  *) echo "Unknown release tool: $tool" >&2; exit 1 ;;
esac
destination="$project_dir/.local/release-tools/${tool}_${version}_${platform}"
if [[ ! -x "$destination/$tool" ]]; then
  mkdir -p "$destination"
  scratch="$(mktemp -d)"
  trap 'rm -rf "$scratch"' EXIT
  curl --fail --silent --show-error --location --retry 3 \
    "https://github.com/${repository}/releases/download/v${version}/${asset}" -o "$scratch/$asset"
  (cd "$scratch"; printf '%s  %s\n' "$checksum" "$asset" | shasum -a 256 -c -) >&2
  tar -xzf "$scratch/$asset" -C "$scratch" "$member"
  if [[ "$member" != "$tool" ]]; then
    mv "$scratch/$member" "$scratch/$tool"
  fi
  chmod 755 "$scratch/$tool"
  mv "$scratch/$tool" "$destination/$tool"
  rm -rf "$scratch"
  trap - EXIT
fi
exec "$destination/$tool" "$@"
