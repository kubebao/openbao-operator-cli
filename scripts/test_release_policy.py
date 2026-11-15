"""Exercise publication boundaries and externally supplied release identities."""

import unittest
from unittest import mock

import release_policy
import publish_release
import promote_packages
from release_policy import apple_signing_mode, validate_immutable, validate_publication, validate_tag


class ReleasePolicyTests(unittest.TestCase):
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
