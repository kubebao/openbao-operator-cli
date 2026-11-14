"""Verify distribution contents and exercise the archive for the current host."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import tarfile
import tempfile

PLATFORMS = ("linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64")


def read_checksums(directory):
    checksums = {}
    for line in (directory / "checksums.txt").read_text(encoding="utf-8").splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([A-Za-z0-9_.+-]+)", line)
        if not match or match[2] in checksums:
            raise ValueError("Invalid or duplicate checksum entry.")
        digest, name = match.groups()
        path = directory / name
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            raise ValueError(f"Checksum mismatch or unsafe artifact: {name}")
        checksums[name] = digest
    return checksums


def read_archive(path):
    with tarfile.open(path, "r:gz") as archive:
        members = archive.getmembers()
        if {member.name for member in members} != {"kubebao", "LICENSE", "THIRD_PARTY_LICENSES"} or len(members) != 3:
            raise ValueError(f"Unexpected archive entries: {path.name}")
        if any(not member.isfile() or member.size > 128 * 1024 * 1024 for member in members):
            raise ValueError(f"Unsafe archive member: {path.name}")
        binary = archive.getmember("kubebao")
        if not binary.mode & 0o111:
            raise ValueError(f"Binary is not executable: {path.name}")
        return archive.extractfile(binary).read()


def verify(directory):
    metadata = json.loads((directory / "metadata.json").read_text(encoding="utf-8"))
    version = metadata["version"]
    checksums = read_checksums(directory)
    expected = {f"kubebao_{version}_{target}.{suffix}" for target in PLATFORMS for suffix in ("tar.gz", "spdx.json")}
    staged = (directory / "kubebao.yaml").is_file()
    if staged:
        expected |= {"metadata.json", "kubebao.yaml", "kubebao.rb"}
    if set(checksums) != expected:
        raise ValueError("Unexpected distribution checksum entries.")
    krew = (directory / ("kubebao.yaml" if staged else "krew/kubebao.yaml")).read_text(encoding="utf-8")
    cask = (directory / ("kubebao.rb" if staged else "homebrew/Casks/kubebao.rb")).read_text(encoding="utf-8")
    if f"version: v{version}\n" not in krew or f'version "{version}"' not in cask:
        raise ValueError("Package version does not match the build version.")
    for target in PLATFORMS:
        name = f"kubebao_{version}_{target}.tar.gz"
        binary = read_archive(directory / name)
        url = f"https://github.com/kubebao/openbao-operator-cli/releases/download/v{version}/{name}"
        if url not in krew or checksums[name] not in krew or (target.startswith("darwin_") and checksums[name] not in cask):
            raise ValueError(f"Package metadata does not reference the archive: {name}")
        sbom = json.loads((directory / f"kubebao_{version}_{target}.spdx.json").read_text(encoding="utf-8"))
        names = {package["name"] for package in sbom.get("packages", [])}
        if sbom.get("spdxVersion") != "SPDX-2.3" or "github.com/kubebao/openbao-operator-client" not in names:
            raise ValueError(f"SBOM is missing linked dependencies: {target}")
        binary_digest = hashlib.sha256(binary).hexdigest()
        digests = {checksum["checksumValue"] for file in sbom.get("files", [])
                   for checksum in file.get("checksums", []) if checksum["algorithm"] == "SHA256"}
        if binary_digest not in digests:
            raise ValueError(f"SBOM does not describe the archived executable: {target}")
    return metadata


def smoke(directory, metadata, require_kubectl=False, check_krew=False):
    goos = platform.system().lower()
    goarch = {"aarch64": "arm64", "arm64": "arm64", "x86_64": "amd64"}[platform.machine()]
    target = f"{goos}_{goarch}"
    binary = read_archive(directory / f"kubebao_{metadata['version']}_{target}.tar.gz")
    with tempfile.TemporaryDirectory(prefix="kubebao-dist-") as scratch:
        executable = Path(scratch) / "kubebao"
        executable.write_bytes(binary)
        executable.chmod(0o755)
        plugin = Path(scratch) / "kubectl-kubebao"
        plugin.symlink_to(executable)
        env = {**os.environ, "PATH": f"{scratch}{os.pathsep}{os.environ['PATH']}", "KUBECONFIG": f"{scratch}/absent"}
        commands = ([str(executable)], [str(plugin)])
        if check_krew:
            krew_root = Path(scratch) / "krew"
            env["KREW_ROOT"] = str(krew_root)
            env["PATH"] = f"{krew_root / 'bin'}{os.pathsep}{env['PATH']}"
            manifest = directory / ("kubebao.yaml" if (directory / "kubebao.yaml").is_file() else "krew/kubebao.yaml")
            archive = directory / f"kubebao_{metadata['version']}_{target}.tar.gz"
            subprocess.run([str(Path(__file__).with_name("release-tool.sh").resolve()), "krew", "install",
                            "--manifest", str(manifest.resolve()), "--archive", str(archive.resolve())],
                           env=env, stdin=subprocess.DEVNULL, check=True)
            commands += ([str(krew_root / "bin/kubectl-kubebao")],)
        kubectl = shutil.which("kubectl")
        if require_kubectl and not kubectl:
            raise ValueError("kubectl is required for plugin qualification.")
        if kubectl:
            commands += ([kubectl, "kubebao"],)
        for command in commands:
            result = subprocess.run(command + ["version", "--output", "json"], env=env, capture_output=True, text=True, check=True)
            version = json.loads(result.stdout)
            if result.stderr or any(version[key] != value for key, value in {
                "version": f"v{metadata['version']}", "commit": metadata["commit"], "os": goos, "arch": goarch,
            }.items()):
                raise ValueError("Archive version output does not match build metadata.")
            result = subprocess.run(command + ["doctor", "--help"], env=env, capture_output=True, text=True, check=True)
            expected = "kubectl kubebao doctor" if command != commands[0] else "kubebao doctor"
            if result.stderr or expected not in result.stdout:
                raise ValueError("Invocation help does not match the executable name.")
        invalid = subprocess.run([str(plugin), "unknown-command"], env=env, capture_output=True, text=True, check=False)
        if invalid.returncode != 1 or invalid.stdout or "Error:" not in invalid.stderr:
            raise ValueError("Plugin error output or exit code is incorrect.")
    print(f"Verified archive execution on {target}; kubectl plugin exercised: {bool(kubectl)}.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    parser.add_argument("--smoke", action="store_true")
    parser.add_argument("--require-kubectl", action="store_true")
    parser.add_argument("--krew", action="store_true", help="Install the manifest and local archive into a temporary Krew root")
    args = parser.parse_args()
    metadata = verify(args.directory)
    if args.smoke or args.require_kubectl:
        smoke(args.directory, metadata, args.require_kubectl, args.krew)
    print("Verified four archives, four SPDX SBOMs, checksums, and package metadata.")


if __name__ == "__main__":
    main()
