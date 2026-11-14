#!/usr/bin/env bash
# Build local artifacts and package metadata. This script never publishes.
set -euo pipefail
cd "$(dirname "$0")/.."

mode="${1:-snapshot}"
export KUBEBAO_VERSION="${2:-0.0.0-dev}"
export KUBEBAO_NOTARIZE="${KUBEBAO_NOTARIZE:-false}"
flags=(release --clean --skip=publish)
case "$mode" in
  snapshot)
    python3 - "$KUBEBAO_VERSION" <<'PY'
import sys
sys.path.insert(0, "scripts")
from release_policy import validate_tag
version = sys.argv[1]
validate_tag("v" + version)
if "-" not in version:
    raise ValueError("Snapshot version requires a prerelease suffix, such as 0.0.0-dev.")
PY
    flags+=(--snapshot)
    ;;
  release)
    # GoReleaser requires a clean checkout at a semantic version tag.
    :
    ;;
  *) echo "Usage: scripts/dist.sh [snapshot|release] [snapshot-version]" >&2; exit 1 ;;
esac
./scripts/release-tool.sh syft version >/dev/null
./scripts/release-tool.sh goreleaser "${flags[@]}"
