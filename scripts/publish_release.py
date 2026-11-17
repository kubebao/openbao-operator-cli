"""Publish verified artifacts from a manually approved workflow run."""

import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

import release_policy
from verify_dist import read_checksums, verify


def publish_payload(tag, directory, commit, notes, *, draft=None, prerelease=False):
    """Upload once, compare every downloaded byte, then finalize immutable publication."""
    names = sorted(read_checksums(directory)) + ["checksums.txt", "provenance.sigstore.json"]
    assets = [str(directory / name) for name in names]
    if draft is None:
        command = ["gh", "release", "create", tag, "--draft", "--verify-tag", "--target", commit,
                   "--repo", "kubebao/openbao-operator-cli", "--title", f"kubebao {tag}", "--notes-file", str(notes)]
        if prerelease:
            command.append("--prerelease")
        subprocess.run(command + assets, check=True)
    else:
        fresh = release_policy.github(f"repos/kubebao/openbao-operator-cli/releases/{draft['id']}")
        release_policy.validate_release_please_draft(fresh, tag, commit)
        subprocess.run(["gh", "release", "edit", tag, "--repo", "kubebao/openbao-operator-cli",
                        "--title", f"kubebao {tag}", "--notes-file", str(notes)], check=True)
        # Do not clobber an asset if another writer has changed the draft.
        subprocess.run(["gh", "release", "upload", tag, "--repo", "kubebao/openbao-operator-cli"] + assets, check=True)
    with tempfile.TemporaryDirectory(prefix="kubebao-upload-") as scratch:
        subprocess.run(["gh", "release", "download", tag, "--repo", "kubebao/openbao-operator-cli", "--dir", scratch], check=True)
        if {item.name for item in Path(scratch).iterdir()} != set(names):
            raise ValueError("Uploaded release has an unexpected asset set.")
        for name in names:
            if hashlib.sha256((Path(scratch) / name).read_bytes()).digest() != hashlib.sha256((directory / name).read_bytes()).digest():
                raise ValueError(f"Uploaded artifact differs from the verified build: {name}")
    release_policy.validate_remote_tag(tag, commit)
    subprocess.run(["gh", "release", "edit", tag, "--repo", "kubebao/openbao-operator-cli", "--draft=false",
                    f"--latest={str(not prerelease).lower()}"], check=True)
    published = release_policy.github(f"repos/kubebao/openbao-operator-cli/releases/tags/{tag}")
    if (published.get("draft") or published.get("immutable") is not True or
            published.get("prerelease") is not prerelease or published.get("tag_name") != tag):
        raise ValueError("Release publication, channel, or immutability was not confirmed.")
    print(json.dumps({"url": published["html_url"], "tag": tag, "commit": commit, "immutable": True}))


def main():
    tag, path = sys.argv[1:]
    if os.environ.get("KUBEBAO_PUBLISH") != "true":
        raise ValueError("Publication was not requested.")
    draft = release_policy.main()
    directory = Path(path)
    metadata = verify(directory, expected_kind="release", expected_signing=release_policy.apple_signing_mode(
        os.environ.get("KUBEBAO_NOTARIZE", "false")))
    if metadata["version"] != release_policy.validate_tag(tag):
        raise ValueError("Artifacts do not match the release tag.")
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    if metadata["commit"] != commit:
        raise ValueError("Artifacts do not match the checked-out commit.")
    release_policy.validate_remote_tag(tag, commit)
    bundle = directory / "provenance.sigstore.json"
    if not bundle.is_file():
        raise ValueError("Build provenance bundle is missing.")
    # Verify the identity of the workflow that produced the payload.
    subprocess.run(["gh", "attestation", "verify", str(directory / "checksums.txt"),
                    "--bundle", str(bundle), "--repo", "kubebao/openbao-operator-cli",
                    "--signer-workflow", "kubebao/openbao-operator-cli/.github/workflows/release.yml",
                    "--source-ref", "refs/heads/main", "--source-digest", commit], check=True)
    publish_payload(tag, directory, commit, Path(f"docs/releases/{tag}.md"), draft=draft, prerelease="-" in tag)


if __name__ == "__main__":
    main()
