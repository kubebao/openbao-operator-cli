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

For an internal mirror, retain the original archives, checksums, SBOMs, and provenance bundle. Offline attestation
verification also needs trusted verification material. Follow GitHub's
[offline verification procedure](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/verify-attestations-offline).

## Homebrew

After the official tap contains the stable release:

```sh
brew install --cask kubebao/tap/kubebao
kubebao version
```

Upgrade through the same tap:

```sh
brew upgrade --cask kubebao/tap/kubebao
```

The cask installs the signed macOS release binary. It does not remove quarantine attributes or disable Gatekeeper.
Use a direct versioned download when an automation job requires an exact version.

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

## Verify an installation

Run `kubebao version --output json`, or `kubectl kubebao version --output json` for Krew. The result contains the CLI
version, source commit, Go version, operating system, and architecture. Use one installation method for each executable
on `PATH` so upgrades select the intended version.

Windows packages, OS package repositories, container images, and self-update commands are outside the initial
distribution scope. See [operator qualification](client-migration.md#operator-qualification-boundary) for the current
capability limits; installing a binary does not extend that qualification.
