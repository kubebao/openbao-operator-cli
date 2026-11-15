"""Exercise publication boundaries and externally supplied release identities."""

import hashlib
import json
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest import mock

import release_policy
import publish_release
import promote_packages
from release_policy import apple_signing_mode, validate_immutable, validate_publication, validate_tag


class ReleasePolicyTests(unittest.TestCase):
    def draft(self):
        return {"id": 7, "draft": True, "immutable": False, "tag_name": "v0.1.0", "assets": [],
                "target_commitish": "a" * 40, "prerelease": False,
                "author": {"login": "openbao-operator-cli-release[bot]", "type": "Bot"}}

    def test_only_empty_release_app_drafts_can_be_finalized(self):
        valid = self.draft()
        release_policy.validate_release_please_draft(valid, "v0.1.0", "a" * 40)
        mutations = [{"draft": False}, {"immutable": True}, {"tag_name": "v0.2.0"},
                     {"assets": [{"name": "unexpected"}]}, {"target_commitish": "b" * 40},
                     {"prerelease": True}, {"author": {"login": "another-app[bot]", "type": "Bot"}},
                     {"author": {"login": "openbao-operator-cli-release[bot]", "type": "User"}}]
        for mutation in mutations:
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                release_policy.validate_release_please_draft({**valid, **mutation}, "v0.1.0", "a" * 40)

    def test_existing_drafts_require_explicit_mode_and_unique_identity(self):
        with mock.patch("subprocess.check_output", return_value=json.dumps([[self.draft()]])):
            with self.assertRaises(ValueError):
                release_policy.existing_release("v0.1.0", "a" * 40)
            self.assertEqual(release_policy.existing_release("v0.1.0", "a" * 40, True)["id"], 7)
        for releases in ([], [self.draft(), self.draft()]):
            with mock.patch("subprocess.check_output", return_value=json.dumps([releases])), self.assertRaises(ValueError):
                release_policy.existing_release("v0.1.0", "a" * 40, True)

    def test_release_pr_requires_reviewed_notes(self):
        with tempfile.TemporaryDirectory() as scratch:
            root = Path(scratch)
            manifest = root / ".release-please-manifest.json"
            manifest.write_text(json.dumps({".": "0.0.0"}))
            release_policy.check_release_notes(root)
            manifest.write_text(json.dumps({".": "0.1.0"}))
            with self.assertRaises(ValueError):
                release_policy.check_release_notes(root)
            notes = root / "docs/releases/v0.1.0.md"
            notes.parent.mkdir(parents=True)
            notes.write_text("\n")
            with self.assertRaises(ValueError):
                release_policy.check_release_notes(root)
            notes.write_text("Reviewed notes.\n")
            release_policy.check_release_notes(root)

    def test_remote_tag_is_checked_against_the_payload_commit(self):
        with mock.patch("release_policy.github", return_value={"object": {"type": "commit", "sha": "b" * 40}}):
            with self.assertRaises(ValueError):
                release_policy.validate_remote_tag("v0.1.0", "a" * 40)

    def test_apple_signing_is_an_explicit_boolean(self):
        self.assertEqual(apple_signing_mode("false"), "none")
        self.assertEqual(apple_signing_mode("true"), "developer-id-notarized")
        for invalid in (None, "", "True", "yes", "auto"):
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                apple_signing_mode(invalid)

    def test_publisher_without_opt_in_does_not_call_external_tools(self):
        with mock.patch.dict("os.environ", {}, clear=True), \
             mock.patch("sys.argv", ["publish_release.py", "v0.1.0", "payload"]), \
             mock.patch("subprocess.run") as run, mock.patch("subprocess.check_output") as output:
            with self.assertRaises(ValueError):
                publish_release.main()
            run.assert_not_called()
            output.assert_not_called()

    def test_prerelease_promotion_does_not_call_external_tools(self):
        with mock.patch("sys.argv", ["promote_packages.py", "v0.1.0-rc.1", "both"]), \
             mock.patch("promote_packages.github") as github, mock.patch("subprocess.run") as run:
            with self.assertRaises(ValueError):
                promote_packages.main()
            github.assert_not_called()
            run.assert_not_called()

    def test_mismatched_workflow_source_fails_before_api_access(self):
        with mock.patch.dict("os.environ", {"GITHUB_REF": "refs/heads/main", "GITHUB_SHA": "different"}, clear=True), \
             mock.patch("sys.argv", ["release_policy.py", "v0.1.0"]), \
             mock.patch("subprocess.check_output", return_value="built-commit\n"), \
             mock.patch("release_policy.github") as github:
            with self.assertRaises(ValueError):
                release_policy.main()
            github.assert_not_called()

    def test_valid_versions(self):
        for tag in ("v0.1.0", "v1.2.3-rc.1", "v1.2.3-alpha-beta"):
            self.assertEqual(validate_tag(tag), tag[1:])

    def test_reject_unsafe_or_ambiguous_versions(self):
        for tag in ("main", "0.1.0", "v01.0.0", "v1.2.3-rc.01", "v1.2.3/other", "v1.2.3\n", "v1.2.3;echo bad"):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                validate_tag(tag)

    def test_publication_requires_opt_in_and_public_repository(self):
        public = {"full_name": "kubebao/openbao-operator-cli", "visibility": "public"}
        validate_publication(public, "true")
        for repository, enabled in (
            (public, None), (public, "false"),
            ({**public, "visibility": "private"}, "true"),
            ({**public, "full_name": "another/project"}, "true"),
            ({}, "true"),
        ):
            with self.subTest(repository=repository, enabled=enabled), self.assertRaises(ValueError):
                validate_publication(repository, enabled)

    def test_publication_requires_immutable_releases(self):
        validate_immutable({"enabled": True})
        for settings in (None, {}, {"enabled": False}, {"enabled": "true"}):
            with self.subTest(settings=settings), self.assertRaises(ValueError):
                validate_immutable(settings)


class PayloadPublicationTests(unittest.TestCase):
    def exercise(self, *, draft=False, corrupt=False, extra=False, moved_tag=False):
        with tempfile.TemporaryDirectory() as scratch:
            directory = Path(scratch) / "payload"
            directory.mkdir()
            (directory / "archive.tar.gz").write_bytes(b"verified-artifact")
            (directory / "checksums.txt").write_text(
                hashlib.sha256(b"verified-artifact").hexdigest() + "  archive.tar.gz\n")
            (directory / "provenance.sigstore.json").write_text("verified-proof")
            notes = Path(scratch) / "notes.md"
            notes.write_text("Reviewed notes.")
            commands = []

            def run(command, **_kwargs):
                commands.append(command)
                if command[:3] == ["gh", "release", "download"]:
                    destination = Path(command[command.index("--dir") + 1])
                    for path in directory.iterdir():
                        shutil.copyfile(path, destination / path.name)
                    if corrupt:
                        (destination / "archive.tar.gz").write_bytes(b"different")
                    if extra:
                        (destination / "unlisted").write_text("unexpected")

            release = ReleasePolicyTests().draft() if draft else None
            published = {"draft": False, "immutable": True, "prerelease": not draft,
                         "tag_name": "v0.1.0", "html_url": "fixture-release"}
            def github(path):
                return release if path.endswith("/7") else published

            with mock.patch("subprocess.run", side_effect=run), \
                    mock.patch("release_policy.github", side_effect=github), \
                    mock.patch("release_policy.validate_remote_tag", side_effect=ValueError("tag moved") if moved_tag else None):
                if corrupt or extra or moved_tag:
                    with self.assertRaises(ValueError):
                        publish_release.publish_payload("v0.1.0", directory, "a" * 40, notes, draft=release, prerelease=not draft)
                else:
                    publish_release.publish_payload("v0.1.0", directory, "a" * 40, notes, draft=release, prerelease=not draft)
            return commands

    def test_edge_does_not_become_latest_or_clobber_assets(self):
        commands = self.exercise()
        self.assertTrue(any("--prerelease" in command for command in commands))
        self.assertTrue(any("--latest=false" in command for command in commands))
        self.assertFalse(any("--latest=true" in command or "--clobber" in command for command in commands))

    def test_release_please_draft_is_uploaded_without_recreation(self):
        commands = self.exercise(draft=True)
        self.assertFalse(any(command[:3] == ["gh", "release", "create"] for command in commands))
        self.assertTrue(any(command[:3] == ["gh", "release", "upload"] for command in commands))
        self.assertTrue(any("--latest=true" in command for command in commands))

    def test_bad_download_or_moved_tag_leaves_the_draft_unpublished(self):
        for failure in ({"corrupt": True}, {"extra": True}, {"moved_tag": True}):
            with self.subTest(failure=failure):
                commands = self.exercise(**failure)
                self.assertFalse(any("--draft=false" in command for command in commands))
