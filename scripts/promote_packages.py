"""Open package update pull requests for an immutable stable release."""

import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from release_policy import github, validate_publication, validate_tag
from verify_dist import verify


def update_package(repository, path, source, tag, upstream=None):
    with tempfile.TemporaryDirectory(prefix="kubebao-package-") as scratch:
        checkout = Path(scratch) / "repository"
        subprocess.run(["gh", "repo", "clone", repository, str(checkout), "--", "--depth=1"], check=True)
        branch = f"chore/kubebao-{tag}"
        target = upstream or repository
        base = github(f"repos/{target}")["default_branch"]
        if upstream:
            subprocess.run(["git", "fetch", "--depth=1", f"https://github.com/{upstream}.git", base], cwd=checkout, check=True)
            subprocess.run(["git", "switch", "-c", branch, "FETCH_HEAD"], cwd=checkout, check=True)
        else:
            subprocess.run(["git", "switch", "-c", branch], cwd=checkout, check=True)
        destination = checkout / path
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, destination)
        subprocess.run(["git", "add", path], cwd=checkout, check=True)
        subprocess.run(["git", "-c", "user.name=KubeBao Release Bot", "-c", "user.email=release-bot@kubebao.org",
                        "commit", "--signoff", "-m", f"chore(kubebao): update to {tag}"], cwd=checkout, check=True)
        subprocess.run(["git", "push", "origin", f"HEAD:refs/heads/{branch}"], cwd=checkout, check=True)
        body = Path(scratch) / "body.md"
        body.write_text(
            f"Update kubebao to [{tag}](https://github.com/kubebao/openbao-operator-cli/releases/tag/{tag}).\n\n"
            "The package definition references the immutable release archives and their verified SHA-256 checksums.\n"
            "Build provenance and package metadata were verified before this update.\n", encoding="utf-8")
        head = f"{repository.split('/')[0]}:{branch}" if upstream else branch
        subprocess.run(["gh", "pr", "create", "--repo", target, "--head", head, "--base", base,
                        "--title", f"chore(kubebao): update to {tag}", "--body-file", str(body)], cwd=checkout, check=True)


def main():
    tag = sys.argv[1]
    channel = sys.argv[2]
    if channel not in ("both", "homebrew", "krew"):
        raise ValueError("Package channel must be both, homebrew, or krew.")
    validate_tag(tag)
    if "-" in tag:
        raise ValueError("Default package channels accept stable releases only.")
    validate_publication(github("repos/kubebao/openbao-operator-cli"), os.environ.get("CLI_PACKAGES_ENABLED"))
    release = github(f"repos/kubebao/openbao-operator-cli/releases/tags/{tag}")
    if release.get("draft") or release.get("prerelease") or release.get("immutable") is not True:
        raise ValueError("Package promotion requires a published immutable stable release.")
    with tempfile.TemporaryDirectory(prefix="kubebao-promotion-") as scratch:
        payload = Path(scratch)
        subprocess.run(["gh", "release", "download", tag, "--repo", "kubebao/openbao-operator-cli", "--dir", scratch], check=True)
        metadata = verify(payload, expected_kind="release")
        if metadata["version"] != tag[1:]:
            raise ValueError("Package metadata does not match the release tag.")
        subprocess.run(["gh", "attestation", "verify", str(payload / "checksums.txt"),
                        "--bundle", str(payload / "provenance.sigstore.json"), "--repo", "kubebao/openbao-operator-cli",
                        "--signer-workflow", "kubebao/openbao-operator-cli/.github/workflows/release.yml"], check=True)
        # The two PRs have independent reviews and are never merged by this script.
        if channel in ("both", "homebrew"):
            update_package("kubebao/homebrew-tap", "Formula/kubebao.rb", payload / "kubebao.rb", tag)
        if channel in ("both", "krew"):
            update_package("kubebao/krew-index", "plugins/kubebao.yaml", payload / "kubebao.yaml", tag,
                           upstream="kubernetes-sigs/krew-index")


if __name__ == "__main__":
    main()
