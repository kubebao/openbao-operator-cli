# Installation

The CLI is unreleased. GitHub downloads, the Homebrew tap, and the Krew index entry are not published yet. Build from
source until a release is available. The package commands below describe installation after publication.

## Build from source

Use the Go version required by `go.mod`:

```sh
make build
./bin/kubebao version --output json
```

The standalone CLI does not require kubectl, the console, or a backend service. Kubernetes commands use the selected
kubeconfig and its configured credential plugins. Help, version, and offline bundle diagnosis require no cluster.

## Download a release

Release archives target Linux and macOS on amd64 and arm64. Select the archive that matches the host. Published
releases include `checksums.txt`, one SPDX SBOM per binary, and a build-provenance bundle. The CLI has its own version;
the release notes identify tested operator versions and capabilities.

Apple Developer ID signing and notarization are optional. Check `appleSigning` in the checksummed `release-info.json`:
`none` means the binaries have no Developer ID signing or notarization from this release process;
`developer-id-notarized` means that the release requested and verified both. The release also includes a checksummed
source archive for source builds. Build provenance and Apple notarization are separate checks.

After the selected release is published, download its files with the GitHub CLI. This example selects Linux/amd64:

```sh
version=0.1.0
platform=linux_amd64
gh release download "v${version}" --repo kubebao/openbao-operator-cli --dir "kubebao-${version}"
cd "kubebao-${version}"
shasum -a 256 -c checksums.txt
gh attestation verify "kubebao_${version}_${platform}.tar.gz" \
  --repo kubebao/openbao-operator-cli \
  --signer-workflow kubebao/openbao-operator-cli/.github/workflows/release.yml \
  --bundle provenance.sigstore.json
```

The checksum check must pass for every listed file. The attestation check must report a verified statement from the
named release workflow. Checksums detect changed files; the attestation binds them to the workflow identity.

After both checks pass, extract and install the binary:

```sh
tar -xzf "kubebao_${version}_${platform}.tar.gz" kubebao LICENSE THIRD_PARTY_LICENSES
mkdir -p "$HOME/.local/bin"
install -m 0755 kubebao "$HOME/.local/bin/kubebao"
```

Add `$HOME/.local/bin` to `PATH` if necessary. Keep the license notices with any redistributed archive.

On macOS, Gatekeeper can block a downloaded binary without Developer ID signing or notarization. If this happens,
use the source-building Homebrew formula or build from source. The installers do not remove quarantine attributes
or disable Gatekeeper. Startup tests on extracted archives do not qualify every browser-download or managed-device
policy path.

For an internal mirror, retain the original archives, checksums, SBOMs, and provenance bundle. Offline attestation
verification also needs trusted verification material. Follow GitHub's
[offline verification procedure](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/verify-attestations-offline).

## Homebrew

After the official tap contains the stable release:

```sh
brew install --formula kubebao/tap/kubebao
kubebao version
```

Upgrade through the same tap:

```sh
brew upgrade --formula kubebao/tap/kubebao
```

The formula builds from the release's checksummed source archive on macOS or Linux. It installs Go as a build
dependency and uses the module's locked dependencies. Use Homebrew 7 or newer and a Go formula that satisfies the
version required by `go.mod`. The installed CLI does not need Go or an Apple Developer account. Building takes longer
than installing a precompiled archive. Use a direct versioned download when an automation job requires an exact version.

## Krew

After the plugin is accepted into the default Krew index:

```sh
kubectl krew install kubebao
kubectl kubebao version
kubectl kubebao status prod -n security --context production
```

Upgrade with `kubectl krew upgrade kubebao`. Krew uses the same executable as the standalone CLI and exposes it as
`kubectl kubebao`. Use downloads or Homebrew when the standalone `kubebao` command is required. Put kubeconfig and
context flags after `kubectl kubebao` so that kubectl forwards them to the plugin.

## Shell completion

`kubebao completion bash|zsh|fish|powershell` prints a completion script. Load it in the shell profile:

```sh
source <(kubebao completion bash)                                   # ~/.bashrc
kubebao completion zsh > "${fpath[1]}/_kubebao"                     # once, then restart zsh
kubebao completion fish > ~/.config/fish/completions/kubebao.fish   # once
```

Completion suggests commands, flags, fixed flag values, kubeconfig contexts, and the cluster `NAME` argument. Names come
from a bounded list of OpenBaoClusters in the namespace that the typed `--kubeconfig`, `--context`, `--namespace`, and
`--api-group` flags select. The read uses a five-second timeout and needs `list` permission on `openbaoclusters`. A failed
read suggests nothing; it never prints errors into the shell. Zsh and fish also show each cluster's OpenBao version and
whether it is paused or in maintenance.

For the kubectl plugin, kubectl 1.26 and newer completes plugin arguments through an executable named
`kubectl_complete-kubebao` on `PATH`:

```sh
cat > ~/.local/bin/kubectl_complete-kubebao <<'SCRIPT'
#!/usr/bin/env sh
exec kubectl-kubebao __complete "$@"
SCRIPT
chmod +x ~/.local/bin/kubectl_complete-kubebao
```

## Verify an installation

Run `kubebao version --output json`, or `kubectl kubebao version --output json` for Krew. The result contains the CLI
version, source commit, Go version, operating system, and architecture. Use one installation method for each executable
on `PATH` so upgrades select the intended version.

Windows packages, OS package repositories, container images, and self-update commands are outside the initial
distribution scope. See [operator qualification](client-migration.md#operator-qualification-boundary) for the current
capability limits; installing a binary does not extend that qualification.
