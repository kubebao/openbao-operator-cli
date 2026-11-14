"""Validate release identities and publication gates before external writes."""

import json
import os
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


def main():
    tag = sys.argv[1]
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
    if os.environ.get("KUBEBAO_PUBLISH") == "true":
        repository = github("repos/kubebao/openbao-operator-cli")
        validate_publication(repository, os.environ.get("CLI_RELEASES_ENABLED"))
        validate_immutable(github("repos/kubebao/openbao-operator-cli/immutable-releases"))
        notes = f"docs/releases/{tag}.md"
        subprocess.run(["git", "ls-files", "--error-unmatch", notes], check=True, stdout=subprocess.DEVNULL)
        with open(notes, encoding="utf-8") as source:
            if not source.read().strip():
                raise ValueError("Commit reviewed release notes before publication.")
        # A read failure must not be treated as an absent release.
        pages = json.loads(subprocess.check_output([
            "gh", "api", "--paginate", "--slurp", "repos/kubebao/openbao-operator-cli/releases?per_page=100",
        ], text=True))
        releases = [release for page in pages for release in page]
        if any(release["tag_name"] == tag for release in releases):
            raise ValueError("A release already exists for this tag; inspect it before retrying.")
    print(f"Validated {tag} at {commit}.")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, subprocess.CalledProcessError) as error:
        print(f"Release validation failed: {error}", file=sys.stderr)
        sys.exit(1)
