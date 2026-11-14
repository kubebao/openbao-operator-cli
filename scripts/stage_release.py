"""Stage the public payload without build directories or rendered configuration."""

import hashlib
from pathlib import Path
import shutil
import sys

from verify_dist import read_checksums, verify


def main():
    directory = Path(sys.argv[1])
    verify(directory)
    destination = directory / "release"
    destination.mkdir()  # Fail rather than combine artifacts from separate runs.
    paths = [directory / name for name in read_checksums(directory)]
    paths += [directory / "metadata.json", directory / "homebrew/Casks/kubebao.rb", directory / "krew/kubebao.yaml"]
    checksums = []
    for path in sorted(paths, key=lambda item: item.name):
        shutil.copyfile(path, destination / path.name)
        checksums.append(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n")
    (destination / "checksums.txt").write_text("".join(checksums), encoding="utf-8")
    verify(destination)
    print(f"Staged {len(paths)} artifacts and checksums in {destination}.")


if __name__ == "__main__":
    main()
