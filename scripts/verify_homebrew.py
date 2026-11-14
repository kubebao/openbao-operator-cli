"""Exercise the generated formula without installing a package on the host."""

import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile

from verify_dist import verify


def main():
    directory = Path(sys.argv[1]).resolve()
    metadata = verify(directory)
    library = Path(subprocess.check_output(["brew", "--repository"], text=True).strip()) / "Library"
    goroot = Path(subprocess.check_output(["go", "env", "GOROOT"], text=True).strip())
    # Homebrew loads Ruby formulae only from trusted package roots. Create and
    # remove a unique local tap directory; do not register a tap or install it.
    with tempfile.TemporaryDirectory(prefix="kubebao-validation-", dir=library / "Taps") as tap_scratch, \
            tempfile.TemporaryDirectory(prefix="kubebao-formula-") as scratch:
        tap = Path(tap_scratch) / "homebrew-fixture"
        formula = tap / "Formula/kubebao.rb"
        formula.parent.mkdir(parents=True)
        source_formula = directory / ("kubebao.rb" if (directory / "kubebao.rb").is_file() else "homebrew/Formula/kubebao.rb")
        shutil.copyfile(source_formula, formula)
        with tarfile.open(directory / f"kubebao_{metadata['version']}_source.tar.gz", "r:gz") as archive:
            archive.extractall(scratch, filter="data")
        root = Path(scratch)
        env = {**os.environ, "HOMEBREW_NO_AUTO_UPDATE": "1", "HOMEBREW_NO_ANALYTICS": "1", "HOMEBREW_DEVELOPER": "1",
               "HOMEBREW_LOGS": str(root / "logs"), "HOMEBREW_USER_CONFIG_HOME": str(root / "config"),
               "GOTOOLCHAIN": "local", "PATH": f"{goroot / 'bin'}{os.pathsep}{os.environ['PATH']}"}
        subprocess.run(["brew", "trust", "--formula", f"{Path(tap_scratch).name}/fixture/kubebao"], env=env, check=True)
        script = Path(__file__).with_suffix(".rb").resolve()
        subprocess.run(["brew", "ruby", str(script), str(formula), str(root / f"kubebao_{metadata['version']}_source"),
                        str(root / "prefix"), str(root / "test"), str(goroot / "bin")], env=env, check=True)


if __name__ == "__main__":
    main()
