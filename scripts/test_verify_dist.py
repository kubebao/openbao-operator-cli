"""Reject altered artifacts and unsafe archive members before execution."""

import hashlib
import io
from pathlib import Path
import tarfile
import tempfile
import unittest

from verify_dist import read_archive, read_checksums


class DistributionTests(unittest.TestCase):
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

    def test_archive_rejects_links_and_unexpected_entries(self):
        with tempfile.TemporaryDirectory() as scratch:
            path = Path(scratch) / "archive.tar.gz"
            for unsafe in ("../escape", "kubebao"):
                with tarfile.open(path, "w:gz") as archive:
                    for name in ("kubebao", "LICENSE", "THIRD_PARTY_LICENSES"):
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
