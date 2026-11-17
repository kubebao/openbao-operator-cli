"""Exercise edge trust boundaries without creating tags or publishing assets."""

import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock
import urllib.error

import edge_release as edge

SHA = "a" * 40


def run(run_id=42, **changes):
    return {"id": run_id, "run_attempt": 1, "head_sha": SHA, "head_branch": "main", "event": "push",
            "head_repository": {"full_name": edge.REPOSITORY}, "status": "completed", "conclusion": "success", **changes}


class EdgeTests(unittest.TestCase):
    def test_identity_is_unique_and_cannot_inject_paths_or_outputs(self):
        self.assertEqual(edge.edge_version(SHA, 42, 2), "0.0.0-edge.42.2.gaaaaaaaaaaaa")
        for sha, identifier, attempt in (("a" * 12, 42, 1), (SHA, 0, 1), (SHA, "01", 1), (SHA, 42, "1\n")):
            with self.subTest(identity=(sha, identifier, attempt)), self.assertRaises(ValueError):
                edge.edge_version(sha, identifier, attempt)

    def test_gate_off_has_no_api_or_command_access(self):
        with tempfile.TemporaryDirectory() as scratch, \
                mock.patch.dict(os.environ, {"GITHUB_OUTPUT": str(Path(scratch) / "output")}, clear=True), \
                mock.patch("edge_release.api") as api, mock.patch("subprocess.run") as command:
            edge.select_source()
            self.assertEqual((Path(scratch) / "output").read_text(), "ready=false\n")
            with self.assertRaises(ValueError):
                edge.publish(Path(scratch), SHA, 42, 1)
            api.assert_not_called()
            command.assert_not_called()

    def test_latest_failed_run_never_falls_back_to_older_success(self):
        self.assertIsNone(edge.latest_success([run(41), run(42, conclusion="failure")], SHA))
        for changed in ({"event": "pull_request"}, {"head_branch": "feature"},
                        {"head_repository": {"full_name": "fork/project"}}, {"head_sha": "b" * 40}):
            with self.subTest(changed=changed):
                self.assertIsNone(edge.latest_success([run(**changed)], SHA))

    def source_api(self, path, *, skip=False, moved=False):
        if path == "branches/main":
            return {"commit": {"sha": "b" * 40 if moved else SHA}}
        if "workflows/" in path:
            return {"workflow_runs": [run(43 if "codeql" in path else 42)]}
        if "/43/" in path:
            return {"jobs": [{"name": "Analyze Go", "status": "completed", "conclusion": "skipped" if skip else "success"}]}
        return {"jobs": [{"name": name, "status": "completed", "conclusion": "success"} for name in (
            "Secret scan", "Go", "Go vulnerabilities", "Dependency licenses", "Distribution archives", "Edge candidate provenance")]}

    def test_current_main_requires_real_check_and_codeql_job_success(self):
        with mock.patch("edge_release.api", side_effect=self.source_api):
            self.assertEqual(edge.qualified_source(SHA)["id"], 42)
        for failure in ({"skip": True}, {"moved": True}):
            with mock.patch("edge_release.api", side_effect=lambda path: self.source_api(path, **failure)):
                self.assertIsNone(edge.qualified_source(SHA))

    def test_api_failure_is_not_treated_as_missing_release(self):
        with mock.patch.dict(os.environ, {"GH_TOKEN": "fixture-token"}), \
                mock.patch("urllib.request.urlopen", side_effect=urllib.error.HTTPError("fixture", 403, "private-provider-error", {}, None)):
            with self.assertRaisesRegex(ValueError, "HTTP 403") as raised:
                edge.api("releases/tags/v0.1.0", optional=True)
            self.assertNotIn("private-provider-error", str(raised.exception))
        with mock.patch.dict(os.environ, {"GH_TOKEN": "fixture-token"}), \
                mock.patch("urllib.request.urlopen", side_effect=urllib.error.HTTPError("fixture", 404, "missing", {}, None)):
            self.assertIsNone(edge.api("releases/tags/v0.1.0", optional=True))

    def test_provenance_failure_stops_before_parsing_the_payload(self):
        with mock.patch("edge_release.verify_proof", side_effect=ValueError("invalid proof")), \
                mock.patch("edge_release.verify") as verify:
            with self.assertRaises(ValueError):
                edge.verify_candidate(Path("absent"), SHA, 42, 1, proof=True)
            verify.assert_not_called()

    def test_candidate_rejects_mismatched_source_and_unlisted_files(self):
        with tempfile.TemporaryDirectory() as scratch:
            directory = Path(scratch)
            (directory / "checksums.txt").write_text("fixture")
            (directory / "archive").write_text("fixture")
            metadata = {"commit": SHA, "version": edge.edge_version(SHA, 42, 1)}
            with mock.patch("edge_release.verify", return_value=metadata), \
                    mock.patch("edge_release.read_checksums", return_value={"archive": "fixture"}):
                edge.verify_candidate(directory, SHA, 42, 1)
                with self.assertRaises(ValueError):
                    edge.verify_candidate(directory, SHA, 42, 2)
                (directory / "extra").write_text("unlisted")
                with self.assertRaises(ValueError):
                    edge.verify_candidate(directory, SHA, 42, 1)

    def test_proof_pins_workflow_ref_and_source_commit(self):
        with tempfile.TemporaryDirectory() as scratch:
            directory = Path(scratch)
            for name in ("checksums.txt", "provenance.sigstore.json"):
                (directory / name).write_text("fixture")
            with mock.patch("subprocess.run") as command:
                edge.verify_proof(directory, SHA)
            args = command.call_args.args[0]
            self.assertEqual(args[args.index("--source-digest") + 1], SHA)
            self.assertEqual(args[args.index("--source-ref") + 1], "refs/heads/main")
            self.assertEqual(args[args.index("--signer-workflow") + 1], edge.REPOSITORY + "/.github/workflows/check.yml")

    def test_superseded_or_changed_attempt_never_creates_tags(self):
        environment = {"CLI_EDGE_ENABLED": "true", "GITHUB_REPOSITORY": edge.REPOSITORY}
        for current in (None, run(43), run(42, run_attempt=2)):
            with self.subTest(current=current), mock.patch.dict(os.environ, environment, clear=True), \
                    mock.patch("edge_release.api", side_effect=[{"full_name": edge.REPOSITORY, "visibility": "public"}, {"enabled": True}]), \
                    mock.patch("edge_release.verify_candidate"), mock.patch("edge_release.qualified_source", return_value=current), \
                    mock.patch("subprocess.run") as command:
                if current is None:
                    edge.publish(Path("payload"), SHA, 42, 1)
                else:
                    with self.assertRaises(ValueError):
                        edge.publish(Path("payload"), SHA, 42, 1)
                command.assert_not_called()

    def test_duplicate_events_require_an_immutable_prerelease_at_the_same_tag(self):
        release = {"draft": False, "immutable": True, "prerelease": True, "tag_name": "v" + edge.edge_version(SHA, 42, 1)}
        with mock.patch("edge_release.api", return_value=release), mock.patch("edge_release.validate_remote_tag"):
            self.assertTrue(edge.already_published(edge.edge_version(SHA, 42, 1), SHA))
        for mutation in ({"draft": True}, {"immutable": False}, {"prerelease": False}):
            with mock.patch("edge_release.api", return_value={**release, **mutation}), self.assertRaises(ValueError):
                edge.already_published(edge.edge_version(SHA, 42, 1), SHA)

    def test_publication_creates_a_new_tag_and_reuses_the_verified_payload(self):
        environment = {"CLI_EDGE_ENABLED": "true", "GITHUB_REPOSITORY": edge.REPOSITORY}
        with mock.patch.dict(os.environ, environment, clear=True), \
                mock.patch("edge_release.api", side_effect=[{"full_name": edge.REPOSITORY, "visibility": "public"}, {"enabled": True}]), \
                mock.patch("edge_release.verify_candidate") as verify, \
                mock.patch("edge_release.qualified_source", return_value=run()), \
                mock.patch("edge_release.already_published", return_value=False), \
                mock.patch("edge_release.publish_payload") as upload, mock.patch("subprocess.run") as command:
            edge.publish(Path("qualified-payload"), SHA, 42, 1)
        verify.assert_called_once_with(Path("qualified-payload"), SHA, 42, 1, proof=True)
        self.assertEqual(command.call_count, 1)
        self.assertEqual(command.call_args.args[0][-2:], ["-f", "sha=" + SHA])
        self.assertEqual(upload.call_args.args[1], Path("qualified-payload"))
        self.assertTrue(upload.call_args.kwargs["prerelease"])


if __name__ == "__main__":
    unittest.main()
