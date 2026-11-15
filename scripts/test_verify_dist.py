"""Reject altered artifacts and unsafe archive members before execution."""

import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

from prepare_distribution import render_formula
from verify_dist import COMPLETION_HELPER, completion_helper, read_archive, read_checksums, read_release_info, read_source


class DistributionTests(unittest.TestCase):
    def test_signing_metadata_must_match_the_requested_mode_and_commit(self):
        metadata = {"version": "0.1.0", "commit": "a" * 40}
        info = {"schemaVersion": "v1", "kind": "release", **metadata, "appleSigning": "none",
                "sourceArchive": "kubebao_0.1.0_source.tar.gz"}
        with tempfile.TemporaryDirectory() as scratch:
            root = Path(scratch)
            path = root / "release-info.json"
            path.write_text(json.dumps(info), encoding="utf-8")
            read_release_info(root, metadata, "release", "none")
            with self.assertRaises(ValueError):
                read_release_info(root, metadata, "release", "developer-id-notarized")
            info["appleSigning"] = "developer-id-notarized"
            path.write_text(json.dumps(info), encoding="utf-8")
            read_release_info(root, metadata, "release", "developer-id-notarized")
            with self.assertRaises(ValueError):
                read_release_info(root, metadata, "release", "none")
            info["kind"] = "snapshot"
            path.write_text(json.dumps(info), encoding="utf-8")
            with self.assertRaises(ValueError):
                read_release_info(root, metadata)
            info.update(kind="release", commit="b" * 40)
            path.write_text(json.dumps(info), encoding="utf-8")
            with self.assertRaises(ValueError):
                read_release_info(root, metadata)

    def test_source_archive_rejects_wrong_commit_and_unsafe_paths(self):
        with tempfile.TemporaryDirectory() as scratch:
            path = Path(scratch) / "source.tar.gz"
            for commit, name in (("b" * 40, "kubebao_0.1.0_source/go.mod"),
                                 ("a" * 40, "kubebao_0.1.0_source/../../escape")):
                with tarfile.open(path, "w:gz", pax_headers={"comment": commit}) as archive:
                    member = tarfile.TarInfo(name)
                    member.size = 1
                    archive.addfile(member, io.BytesIO(b"x"))
                with self.subTest(commit=commit, name=name), self.assertRaises(ValueError):
                    read_source(path, "0.1.0", "a" * 40)

    def test_source_formula_rejects_values_that_can_inject_ruby(self):
        template = (Path(__file__).resolve().parent.parent / "packaging/homebrew/kubebao.rb.tmpl").read_text(encoding="utf-8")
        formula = render_formula(template, "0.1.0", "a" * 40, "b" * 64)
        self.assertIn("kubebao_0.1.0_source.tar.gz", formula)
        self.assertIn('depends_on "go" => :build', formula)
        self.assertNotIn("cask ", formula)
        for version, commit, digest in (("0.1.0\"; system 'bad'", "a" * 40, "b" * 64),
                                        ("0.1.0", '"', "b" * 64), ("0.1.0", "a" * 40, '"')):
            with self.subTest(version=version, commit=commit, digest=digest), self.assertRaises(ValueError):
                render_formula(template, version, commit, digest)

    def test_checksum_rejects_changes_and_unsafe_paths(self):
        with tempfile.TemporaryDirectory() as scratch:
            root = Path(scratch)
            (root / "artifact").write_bytes(b"original")
            digest = hashlib.sha256(b"original").hexdigest()
            checksums = root / "checksums.txt"
            checksums.write_text(f"{digest}  artifact\n", encoding="utf-8")
            self.assertEqual(read_checksums(root), {"artifact": digest})
            (root / "artifact").write_bytes(b"altered")
            with self.assertRaises(ValueError):
                read_checksums(root)
            for name in ("../artifact", "/artifact", "nested/artifact"):
                checksums.write_text(f"{digest}  {name}\n", encoding="utf-8")
                with self.subTest(name=name), self.assertRaises(ValueError):
                    read_checksums(root)

    def write_archive(self, path, entries):
        with tarfile.open(path, "w:gz") as archive:
            for name, (content, mode) in entries.items():
                member = tarfile.TarInfo(name)
                member.mode, member.size = mode, len(content)
                archive.addfile(member, io.BytesIO(content))

    def test_archive_requires_the_unchanged_executable_completion_helper(self):
        valid = {"kubebao": (b"binary", 0o755), COMPLETION_HELPER: (completion_helper(), 0o755),
                 "LICENSE": (b"x", 0o644), "THIRD_PARTY_LICENSES": (b"x", 0o644)}
        with tempfile.TemporaryDirectory() as scratch:
            path = Path(scratch) / "archive.tar.gz"
            self.write_archive(path, valid)
            self.assertEqual(read_archive(path), b"binary")
            self.assertEqual(read_archive(path, COMPLETION_HELPER), completion_helper())
            altered = {**valid, COMPLETION_HELPER: (b"#!/bin/sh\nexec rm -rf /\n", 0o755)}
            missing = {name: value for name, value in valid.items() if name != COMPLETION_HELPER}
            for name, entries in (("altered", altered), ("not executable", {**valid, COMPLETION_HELPER: (completion_helper(), 0o644)}),
                                  ("missing", missing)):
                self.write_archive(path, entries)
                with self.subTest(name), self.assertRaises(ValueError):
                    read_archive(path)

    def test_archive_rejects_links_and_unexpected_entries(self):
        with tempfile.TemporaryDirectory() as scratch:
            path = Path(scratch) / "archive.tar.gz"
            for unsafe in ("../escape", "kubebao"):
                with tarfile.open(path, "w:gz") as archive:
                    for name in ("kubebao", COMPLETION_HELPER, "LICENSE", "THIRD_PARTY_LICENSES"):
                        member = tarfile.TarInfo(unsafe if name == "kubebao" else name)
                        member.mode = 0o755
                        if unsafe == "kubebao" and name == "kubebao":
                            member.type = tarfile.SYMTYPE
                            member.linkname = "/outside"
                        else:
                            member.size = 1
                        archive.addfile(member, io.BytesIO(b"x"))
                with self.subTest(unsafe=unsafe), self.assertRaises(ValueError):
                    read_archive(path)
