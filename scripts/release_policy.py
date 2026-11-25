"""Validate release identities and publication gates before external writes."""

import json
import os
from pathlib import Path
import re
import subprocess
import sys


def validate_tag(tag):
    core = r"(0|[1-9][0-9]*)"
    if not re.fullmatch(rf"v{core}\.{core}\.{core}(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?", tag):
        raise ValueError("Release tag must be vMAJOR.MINOR.PATCH with an optional prerelease suffix.")
    if "-" in tag:
        for identifier in tag.split("-", 1)[1].split("."):
            if identifier.isdigit() and len(identifier) > 1 and identifier.startswith("0"):
                raise ValueError("Numeric prerelease identifiers must not have leading zeros.")
    return tag[1:]


def validate_publication(repository, enabled):
    if enabled != "true":
        raise ValueError("Publication requires the repository gate to be true.")
    if repository.get("full_name") != "kubebao/openbao-operator-cli" or repository.get("visibility") != "public":
        raise ValueError("Publication requires the public kubebao/openbao-operator-cli repository.")


def validate_immutable(immutable):
    if not isinstance(immutable, dict) or immutable.get("enabled") is not True:
        raise ValueError("Enable immutable releases before publication.")


def apple_signing_mode(enabled):
    if enabled not in ("true", "false"):
        raise ValueError("KUBEBAO_NOTARIZE must be true or false.")
    return "developer-id-notarized" if enabled == "true" else "none"


def github(path):
    return json.loads(subprocess.check_output(["gh", "api", path], text=True))


def validate_release_please_draft(release, tag, commit):
    author = release.get("author", {})
    if (release.get("draft") is not True or release.get("immutable") is True or
            release.get("tag_name") != tag or release.get("assets") != [] or
            release.get("target_commitish") not in (commit, "main") or
            release.get("prerelease") is not ("-" in tag) or
            author.get("login") != "openbao-operator-cli-release[bot]" or author.get("type") != "Bot"):
        raise ValueError("Expected an empty draft from the CLI release App at the selected tag.")
    return release


def existing_release(tag, commit, allow_draft=False):
    # Read failures must not be interpreted as an absent release.
    pages = json.loads(subprocess.check_output([
        "gh", "api", "--paginate", "--slurp", "repos/kubebao/openbao-operator-cli/releases?per_page=100",
    ], text=True))
    matches = [release for page in pages for release in page if release["tag_name"] == tag]
    if allow_draft:
        if len(matches) != 1:
            raise ValueError("Expected exactly one Release Please draft; inspect releases before retrying.")
        return validate_release_please_draft(matches[0], tag, commit)
    if matches:
        raise ValueError("A release already exists for this tag; inspect it before retrying.")
    return None


def validate_remote_tag(tag, commit):
    remote = github(f"repos/kubebao/openbao-operator-cli/git/ref/tags/{tag}")["object"]
    for _ in range(8):
        if remote["type"] == "commit":
            break
        if remote["type"] != "tag":
            raise ValueError("Release tag does not reference a commit.")
        remote = github(f"repos/kubebao/openbao-operator-cli/git/tags/{remote['sha']}")["object"]
    if remote["type"] != "commit" or remote["sha"] != commit:
        raise ValueError("Remote tag does not match the verified build.")


def check_release_notes(root):
    """Block a version PR until its reviewed notes are included in the source."""
    version = json.loads((root / ".release-please-manifest.json").read_text(encoding="utf-8"))["."]
    validate_tag("v" + version)
    if version != "0.0.0":
        notes = root / "docs/releases" / f"v{version}.md"
        if not notes.is_file() or notes.is_symlink() or not notes.read_text(encoding="utf-8").strip():
            raise ValueError(f"Include reviewed release notes in docs/releases/v{version}.md before merging the version PR.")


def main():
    tag = sys.argv[1]
    if tag == "check-notes":
        check_release_notes(Path.cwd())
        return None
    validate_tag(tag)
    apple_signing_mode(os.environ.get("KUBEBAO_NOTARIZE", "false"))
    if os.environ.get("GITHUB_REF") != "refs/heads/main":
        raise ValueError("Dispatch the release workflow from main.")
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    if commit != os.environ.get("GITHUB_SHA"):
        raise ValueError("Release tag must match the main commit selected for this workflow run.")
    tagged = subprocess.check_output(["git", "rev-parse", f"refs/tags/{tag}^{{commit}}"], text=True).strip()
    if tagged != commit:
        raise ValueError("Checkout does not match the selected release tag.")
    subprocess.run(["git", "merge-base", "--is-ancestor", commit, "origin/main"], check=True)
    draft = None
    if os.environ.get("KUBEBAO_PUBLISH") == "true":
        if os.environ.get("GITHUB_REPOSITORY") != "kubebao/openbao-operator-cli":
            raise ValueError("Release publication requires the canonical repository workflow.")
        repository = github("repos/kubebao/openbao-operator-cli")
        validate_publication(repository, os.environ.get("CLI_RELEASES_ENABLED"))
        validate_immutable(github("repos/kubebao/openbao-operator-cli/immutable-releases"))
        notes = f"docs/releases/{tag}.md"
        subprocess.run(["git", "ls-files", "--error-unmatch", notes], check=True, stdout=subprocess.DEVNULL)
        if Path(notes).is_symlink() or not Path(notes).is_file():
            raise ValueError("Release notes must be a regular tracked file.")
        with open(notes, encoding="utf-8") as source:
            if not source.read().strip():
                raise ValueError("Commit reviewed release notes before publication.")
        draft = existing_release(tag, commit, os.environ.get("RELEASE_PLEASE_DRAFT") == "true")
    print(f"Validated {tag} at {commit}.")
    return draft


if __name__ == "__main__":
    try:
        main()
    except (ValueError, subprocess.CalledProcessError) as error:
        print(f"Release validation failed: {error}", file=sys.stderr)
        sys.exit(1)
