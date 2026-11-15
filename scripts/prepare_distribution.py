"""Generate a source formula and record the requested distribution signing mode."""

import hashlib
import json
import os
from pathlib import Path
import re
from string import Template
import sys

from release_policy import apple_signing_mode, validate_tag
from verify_dist import read_checksums


def render_formula(template, version, commit, source_sha256):
    validate_tag("v" + version)
    if not re.fullmatch(r"[0-9a-f]{40}", commit) or not re.fullmatch(r"[0-9a-f]{64}", source_sha256):
        raise ValueError("Formula generation requires a full commit and SHA-256 checksum.")
    return Template(template).substitute(
        version=version, commit=commit, source_sha256=source_sha256,
        source_archive=f"kubebao_{version}_source.tar.gz",
    )


def prepare(directory, kind, signing):
    if kind not in ("snapshot", "release") or signing not in ("none", "developer-id-notarized"):
        raise ValueError("Invalid distribution kind or Apple signing mode.")
    if kind == "snapshot" and signing != "none":
        raise ValueError("Snapshots must not request Apple signing.")
    metadata = json.loads((directory / "metadata.json").read_text(encoding="utf-8"))
    checksums = read_checksums(directory)
    source = f"kubebao_{metadata['version']}_source.tar.gz"
    if source not in checksums:
        raise ValueError("The source archive must be checksummed before formula generation.")
    template = Path(__file__).resolve().parent.parent / "packaging/homebrew/kubebao.rb.tmpl"
    formula = render_formula(template.read_text(encoding="utf-8"), metadata["version"], metadata["commit"], checksums[source])
    destination = directory / "homebrew/Formula/kubebao.rb"
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(formula, encoding="utf-8")
    info = {
        "schemaVersion": "v1", "kind": kind, "version": metadata["version"], "commit": metadata["commit"],
        "appleSigning": signing, "sourceArchive": source,
    }
    info_path = directory / "release-info.json"
    info_path.write_text(json.dumps(info, indent=2) + "\n", encoding="utf-8")
    checksums[info_path.name] = hashlib.sha256(info_path.read_bytes()).hexdigest()
    (directory / "checksums.txt").write_text(
        "".join(f"{digest}  {name}\n" for name, digest in sorted(checksums.items())), encoding="utf-8")


if __name__ == "__main__":
    prepare(Path(sys.argv[1]), sys.argv[2], apple_signing_mode(os.environ.get("KUBEBAO_NOTARIZE", "false")))
