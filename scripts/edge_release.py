"""Select, verify, and publish immutable CLI snapshots from qualified main runs."""

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import urllib.error
import urllib.parse
import urllib.request

from publish_release import publish_payload
from release_policy import validate_immutable, validate_publication, validate_remote_tag
from verify_dist import read_checksums, verify

REPOSITORY = "kubebao/openbao-operator-cli"


def edge_version(sha, run_id, attempt):
    if (not re.fullmatch(r"[0-9a-f]{40}", sha) or
            any(not re.fullmatch(r"[1-9][0-9]*", str(value)) for value in (run_id, attempt))):
        raise ValueError("Edge identity requires a full commit and positive run ID and attempt.")
    return f"0.0.0-edge.{run_id}.{attempt}.g{sha[:12]}"


def api(path, *, optional=False):
    request = urllib.request.Request(f"https://api.github.com/repos/{REPOSITORY}/{path}", headers={
        "Authorization": "Bearer " + os.environ["GH_TOKEN"], "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2026-03-10", "User-Agent": "kubebao-cli-edge"})
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        error.close()
        if optional and error.code == 404:
            return None
        raise ValueError(f"GitHub request failed (HTTP {error.code}); publication remains blocked.") from None


def latest_success(runs, sha):
    candidates = [run for run in runs if run.get("head_sha") == sha and
                  run.get("event") in ("push", "workflow_dispatch") and run.get("head_branch") == "main" and
                  (run.get("head_repository") or {}).get("full_name") == REPOSITORY]
    if not candidates:
        return None
    latest = max(candidates, key=lambda run: run["id"])
    return latest if latest.get("status") == "completed" and latest.get("conclusion") == "success" else None


def qualified_source(sha):
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise ValueError("Expected a full main commit.")
    if api("branches/main")["commit"]["sha"] != sha:
        return None
    query = urllib.parse.urlencode({"head_sha": sha, "branch": "main", "per_page": 100})
    check = latest_success(api("actions/workflows/check.yml/runs?" + query)["workflow_runs"], sha)
    codeql = latest_success(api("actions/workflows/codeql.yml/runs?" + query)["workflow_runs"], sha)
    if check is None or codeql is None:
        return None
    required = ((check, {"Secret scan", "Go", "Go vulnerabilities", "Dependency licenses",
                         "Distribution archives", "Edge candidate provenance"}), (codeql, {"Analyze Go"}))
    for run, names in required:
        jobs = api(f'actions/runs/{run["id"]}/attempts/{run["run_attempt"]}/jobs?per_page=100')["jobs"]
        passed = {job["name"] for job in jobs if job.get("status") == "completed" and job.get("conclusion") == "success"}
        if not names.issubset(passed):
            return None
    return check


def already_published(version, sha):
    tag = "v" + version
    release = api(f"releases/tags/{tag}", optional=True)
    if release is None:
        return False
    if (release.get("draft") is not False or release.get("immutable") is not True or
            release.get("prerelease") is not True or release.get("tag_name") != tag):
        raise ValueError("An incomplete or conflicting edge release exists; inspect it before retrying.")
    validate_remote_tag(tag, sha)
    return True


def select_source():
    ready, run = False, None
    if os.environ.get("CLI_EDGE_ENABLED") == "true":
        if os.environ.get("GITHUB_REPOSITORY") != REPOSITORY:
            raise ValueError("Edge selection requires the canonical repository.")
        validate_publication(api(""), os.environ["CLI_EDGE_ENABLED"])
        sha = os.environ["SOURCE_SHA"]
        run = qualified_source(sha)
        if run is not None:
            ready = not already_published(edge_version(sha, run["id"], run["run_attempt"]), sha)
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
        output.write(f"ready={str(ready).lower()}\n")
        if ready:
            output.write(f'run_id={run["id"]}\nrun_attempt={run["run_attempt"]}\n')
    if not ready:
        print("Edge is disabled, superseded, already published, or waiting for current Check and CodeQL success.")


def verify_proof(directory, sha):
    for name, limit in (("checksums.txt", 32768), ("provenance.sigstore.json", 4 * 1024 * 1024)):
        path = directory / name
        if path.is_symlink() or not path.is_file() or not 0 < path.stat().st_size <= limit:
            raise ValueError("Missing, unsafe, or oversized CI provenance input.")
    subprocess.run([
        "gh", "attestation", "verify", str(directory / "checksums.txt"),
        "--bundle", str(directory / "provenance.sigstore.json"), "--repo", REPOSITORY,
        "--signer-workflow", f"{REPOSITORY}/.github/workflows/check.yml",
        "--source-ref", "refs/heads/main", "--source-digest", sha,
    ], check=True)


def verify_candidate(directory, sha, run_id, attempt, *, proof=False):
    version = edge_version(sha, run_id, attempt)
    if proof:
        # Authenticate the checksum manifest before reading or executing its payload.
        verify_proof(directory, sha)
    metadata = verify(directory, expected_kind="snapshot", expected_signing="none")
    if metadata["commit"] != sha or metadata["version"] != version:
        raise ValueError("Edge payload does not match the qualified source and run.")
    expected = set(read_checksums(directory)) | {"checksums.txt"}
    if proof:
        expected.add("provenance.sigstore.json")
    if ({path.name for path in directory.iterdir()} != expected or
            any(path.is_symlink() or not path.is_file() for path in directory.iterdir())):
        raise ValueError("Edge payload contains unlisted or unsafe files.")
    return metadata


def publish(directory, sha, run_id, attempt):
    # No reads, external commands, tags, or releases before the opt-in checks.
    if os.environ.get("CLI_EDGE_ENABLED") != "true" or os.environ.get("GITHUB_REPOSITORY") != REPOSITORY:
        raise ValueError("Edge publication requires the canonical repository and CLI_EDGE_ENABLED=true.")
    version = edge_version(sha, run_id, attempt)
    validate_publication(api(""), os.environ["CLI_EDGE_ENABLED"])
    validate_immutable(api("immutable-releases"))
    verify_candidate(directory, sha, run_id, attempt, proof=True)
    # Recheck after native qualification, immediately before the first write.
    current = qualified_source(sha)
    if current is None:
        print("The qualified main build was superseded; nothing was published.")
        return
    if current["id"] != int(run_id) or current["run_attempt"] != int(attempt):
        raise ValueError("A different Check run or attempt now qualifies this source.")
    if already_published(version, sha):
        print("This immutable edge version is already published.")
        return
    tag = "v" + version
    # Creation fails if the tag already exists. Never move a tag or replace assets.
    subprocess.run(["gh", "api", "--method", "POST", f"repos/{REPOSITORY}/git/refs",
                    "-f", f"ref=refs/tags/{tag}", "-f", f"sha={sha}"], check=True, stdout=subprocess.DEVNULL)
    with tempfile.TemporaryDirectory(prefix="kubebao-edge-") as scratch:
        notes = Path(scratch) / "notes.md"
        notes.write_text(
            f"Development snapshot from main at `{sha}`.\n\n"
            f"Qualified by [Check](https://github.com/{REPOSITORY}/actions/runs/{run_id}/attempts/{attempt}) "
            "and native Linux/macOS archive, Krew, and source-build checks on amd64 and arm64. "
            "The source Homebrew formula is tested on macOS.\n\n"
            "This is a prerelease. It does not update stable downloads, Homebrew, or the Krew index. "
            "macOS archives have no Developer ID signature or notarization.\n\n"
            f"[Operator compatibility](https://github.com/{REPOSITORY}/blob/{sha}/docs/compatibility.md) "
            "retains the documented qualification limits.\n", encoding="utf-8")
        publish_payload(tag, directory, sha, notes, prerelease=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("version", "source", "verify", "publish"))
    parser.add_argument("directory", type=Path, nargs="?")
    parser.add_argument("--proof", action="store_true")
    args = parser.parse_args()
    if args.command == "source":
        select_source()
        return
    sha, run_id, attempt = (os.environ[name] for name in ("SOURCE_SHA", "BUILD_RUN_ID", "BUILD_RUN_ATTEMPT"))
    if args.command == "version":
        print(edge_version(sha, run_id, attempt))
    elif args.directory is None:
        parser.error("verify and publish require a payload directory")
    elif args.command == "verify":
        verify_candidate(args.directory, sha, run_id, attempt, proof=args.proof)
    else:
        publish(args.directory, sha, run_id, attempt)


if __name__ == "__main__":
    main()
