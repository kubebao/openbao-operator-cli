# Release procedures

The repository is private, GitHub Actions is disabled, and no release is published. This implementation prepares
distribution without changing those settings. Publication and package-promotion gates default to disabled.

## Artifacts

`.goreleaser.yaml` builds Linux and macOS binaries for amd64 and arm64. Each archive contains `kubebao`, `LICENSE`, and
`THIRD_PARTY_LICENSES`. Syft generates an SPDX 2.3 SBOM for each binary after signing. The Homebrew cask targets macOS;
the Krew manifest covers all four platforms. Both definitions reference the release archives and their SHA-256 hashes.

GoReleaser release publication and package uploads remain disabled in the configuration. Both `make dist` and
`make release-dist` write local files only. `make dist` replaces `dist/` and creates unsigned snapshot artifacts:

```sh
make release-check
make dist DIST_VERSION=0.0.0-dev
make verify-dist
python3 scripts/stage_release.py dist
```

The staging command writes `dist/release/` once. It selects archives, SBOMs, build metadata, and package definitions.
It does not include unpacked build directories or rendered tool configuration. The staged `checksums.txt` covers all
11 payload files. A publishing run adds `provenance.sigstore.json`, which attests those files and the checksum file.
The provenance bundle is not included in its own checksums. Staging refuses an existing output directory.

Distribution tools and host archive hashes are pinned in `scripts/release-tool.sh`. Update a tool version and all four
hashes together after checking its upstream release. These tools are not linked into the CLI and do not change
`THIRD_PARTY_LICENSES`.

## Versioning and qualification

Use independent semantic version tags such as `v0.1.0` and `v0.1.0-rc.1`. Build metadata and numeric prerelease
identifiers with leading zeros are rejected. Prereleases never update default package channels or the latest stable
release pointer. Stable direct downloads remain available for version-pinned automation.

Before a release, record its CLI commit, shared-client version, operator versions and image/CRD identities, Kubernetes
versions, and exercised capabilities. Use [existing qualification evidence](client-migration.md) as the starting point.
The release workflow checks executable startup, help, version output, and process results on all four native platforms.
Those checks do not exercise an operator, prove HA behavior, or qualify production operation. Record command JSON and
support-bundle compatibility changes in the release notes.

## Prepare publication

Perform these steps only after public release is authorized:

1. Approve the repository's public visibility and enable GitHub Actions. The workflows cannot change visibility.
2. Enable immutable releases in GitHub repository settings. Publication checks the setting and fails if it cannot read it.
3. Configure a protected `release` environment with required reviewers, no self-review, and deployment restricted to
   `main`. Use a `release-rehearsal` environment without signing secrets for private rehearsals.
4. Add the signing secrets listed below to the `release` environment. Allow the pinned workflow actions under the
   repository's Actions policy. Keep publication restricted to trusted maintainers.
5. Draft handwritten release notes in chat for review. After approval, commit them as `docs/releases/<tag>.md`, for
   example `docs/releases/v0.1.0.md`. The publishing job requires this tracked, nonempty file.
6. Set the repository variable `CLI_RELEASES_ENABLED=true`. Leave `CLI_PACKAGES_ENABLED` unset until package promotion
   is approved separately.

Required Apple secrets:

| Secret | Value |
| --- | --- |
| `MACOS_SIGN_P12` | Base64-encoded Developer ID Application certificate and private key in PKCS#12 format |
| `MACOS_SIGN_PASSWORD` | Password for the PKCS#12 file |
| `MACOS_NOTARY_ISSUER_ID` | App Store Connect API issuer ID |
| `MACOS_NOTARY_KEY_ID` | App Store Connect API key ID |
| `MACOS_NOTARY_KEY` | Base64-encoded App Store Connect API private key |

Public builds sign and notarize macOS binaries before packaging. Native macOS jobs verify the code signature and
Gatekeeper assessment. The implementation does not bypass quarantine. Apple credentials and hosted signing have not
been qualified by local snapshot builds.

## Run a rehearsal

Once Actions is enabled, create a reviewed version tag at the current `main` commit. Tags must identify a commit on
`main`, and that commit must match the workflow run's source SHA. This keeps the artifact provenance tied to the
compiled source. Tag pushes never publish.

Dispatch the Release workflow from `main`, select the existing tag, and leave `publish=false`. It runs repository
checks, builds a version suffixed with `-rehearsal`, verifies the artifacts, and executes them on four native runners.
The payload remains a workflow artifact. No GitHub release, attestation, or package update is created. A rehearsal
does not use Apple signing secrets or establish signing qualification.

No tag, workflow dispatch, or repository setting change is required for the local snapshot commands above.

## Publish approved artifacts

After the preparation and release approval are complete:

1. Confirm that the selected version tag still matches the `main` commit to dispatch. Review the committed release notes.
2. Dispatch Release from `main` with that tag and `publish=true`. Approve the protected environment requests.
3. Wait for source checks, signed artifact generation, content verification, and all four native runtime jobs.
4. Inspect the resulting release URL, immutable state, version, commit, asset set, and offline provenance bundle.

The publishing job downloads the verified payload and does not rebuild it. It generates build provenance, verifies
the workflow identity, and rechecks the remote tag. It creates a draft with reviewed notes, uploads the payload, and
downloads the complete draft to compare every byte before publication. Stable releases become latest; prereleases do
not. Publication must return a confirmed immutable release.

An existing release causes the preflight check to fail, including an incomplete draft. Inspect a failed run and its
draft before taking a recovery action. The workflow never replaces an existing release or overwrites published assets.
After publication, correct defects with a new version. Reset `CLI_RELEASES_ENABLED=false` to close the publication gate.

## Promote stable packages

Package promotion opens pull requests after an immutable stable release exists. It never merges them.

Before enabling promotion:

1. Create public `kubebao/homebrew-tap` and a public `kubebao/krew-index` fork of `kubernetes-sigs/krew-index`.
   These repositories are not created by this implementation.
2. Configure a protected `release-packages` environment with required reviewers and deployment restricted to `main`.
3. Add `PACKAGE_REPOSITORY_TOKEN` to that environment. It needs content and PR write access to the tap and Krew fork,
   and permission to open a PR against the public upstream Krew index. The default workflow token cannot update these
   repositories. Do not grant administration access.
4. Set `CLI_PACKAGES_ENABLED=true` and dispatch Promote packages from `main` with the published stable tag.

The workflow downloads the published assets, verifies their checksums and provenance, and creates a Homebrew cask PR
and an upstream Krew index PR through the fork. Krew updates start from the current upstream default branch. Review
package installation and index requirements before merging either PR. Default Krew installation becomes available
only after upstream acceptance. Homebrew and Krew packaging are prepared locally; neither public installation path
is qualified before those checks.

The `channel` input selects `both`, `homebrew`, or `krew`. If only one channel completes, inspect its PR and retry the
other channel. Existing update branches are not force-pushed. Reset `CLI_PACKAGES_ENABLED=false` after promotion.
