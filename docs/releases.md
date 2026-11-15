# Release procedures

No release is published; build from source. CI validates the repository without publishing releases or packages. The
`CLI_RELEASES_ENABLED`, `CLI_EDGE_ENABLED`, and `CLI_PACKAGES_ENABLED` repository variables gate tagged releases, edge
publication, and package promotion. All are `false`. An unset variable also disables its channel. Changing repository
visibility does not enable publication. The existing Release and Promote packages workflows remain manually disabled.

## Release channels

| Channel | Source and version | Distribution |
| --- | --- | --- |
| Edge | Qualified current `main`, tagged `v0.0.0-edge.<run>.<attempt>.g<sha>` | Immutable GitHub prerelease downloads; never latest stable |
| Stable | Reviewed Release Please version PR, such as `v0.1.0` | Immutable GitHub release; separate Homebrew and Krew promotion |
| Release candidate | Manually selected tag, such as `v0.1.0-rc.1` | Immutable prerelease downloads; no default package promotion |

CLI versions are independent of operator and console versions. Release Please handles version and changelog PRs.
GoReleaser builds artifacts; the publisher verifies and publishes them. Stable builds use their selected tag, including
when an edge tag refers to the same commit. They are separate builds from edge snapshots, with their own version and
provenance.

### Release Please

The **Release Please** workflow runs on `main` only when `CLI_RELEASES_ENABLED=true` in the public canonical repository.
It uses the organization-owned `openbao-operator-cli-release` GitHub App, installed only on this repository. The App
has Contents, Issues, and Pull requests read/write permissions. Its Actions credentials are:

- `RELEASE_APP_CLIENT_ID`: repository variable with the App's Client ID;
- `RELEASE_APP_PRIVATE_KEY`: repository secret with its PEM private key.

The workflow creates a temporary installation token restricted to this repository and these permissions. It does not
fall back to `GITHUB_TOKEN` for Release Please. Generated PRs receive normal CI. The artifact publisher uses its own
job permissions and OIDC identity.

Release Please maintains `.release-please-manifest.json` and `CHANGELOG.md` from Conventional Commits. The first version
is `0.1.0`. Before 1.0, breaking changes bump the minor version. The `make check` notes gate requires a nonempty
`docs/releases/v<version>.md` when the manifest advances from its initial `0.0.0`. Draft those handwritten notes in chat
for review before committing them to the version PR.

Merging the reviewed version PR creates the tag and a draft GitHub release. Release Please then calls the Release
workflow with publication and draft finalization requested. The publisher accepts only an empty, unpublished draft
from the CLI release App at the selected tag. Protected-environment approval and all existing source, native,
provenance, and immutable-publication checks still apply. The reviewed notes replace the generated draft body.

### Edge

The regular **Check** workflow builds the main snapshot once, verifies the downloaded archives, and stages the public
payload. After every Check job passes, a main-only job attests its checksum manifest and retains the payload with an
offline provenance bundle. These CI artifacts do not create a tag or release and remain available while publishing is
disabled.

When `CLI_EDGE_ENABLED=true`, **Publish Edge** waits for successful Check and CodeQL runs on the current main commit.
It requires successful jobs, including CodeQL analysis and candidate provenance; skipped jobs do not qualify a build.
It authenticates the candidate's checksum manifest against the canonical Check workflow, main ref, and exact source
commit before reading or executing the payload. It exercises the same archives on Linux and macOS, on amd64 and arm64,
including Krew installation and source rebuilds. macOS jobs also build and test the source Homebrew formula.

The publisher rechecks the source and run attempt after native qualification. A superseded main commit is not published.
It creates a unique tag, uploads the exact qualified files without rebuilding, downloads and compares every asset, and
publishes an immutable prerelease with `latest=false`. A repeated completion event skips an already published immutable
version. Existing tags, incomplete drafts, and conflicting assets are never overwritten.

Candidates are named by Check run and attempt. Rerun **all Check jobs** for a fresh candidate; a partial rerun does not
qualify carried artifacts from an earlier attempt. Workflow artifacts expire after seven days, so an expired candidate
also requires a fresh Check run. Both Check and CodeQL support manual runs on `main`.

Edge archives are unsigned for Apple Developer ID purposes. They retain checksums, SPDX SBOMs, source identity, and
verifiable CI provenance. Homebrew and Krew definitions in the payload support explicit testing; edge publication does
not open package promotion PRs or change the default indexes. Edge qualification runs outside ordinary PR checks.
Operator and production qualification limits remain in [compatibility](compatibility.md).

## Artifacts

`.goreleaser.yaml` builds Linux and macOS binaries for amd64 and arm64. Each archive contains `kubebao`, the
`kubectl_complete-kubebao` plugin completion helper, `LICENSE`, and `THIRD_PARTY_LICENSES`. Syft generates an SPDX 2.3
SBOM for each binary after any requested signing. GoReleaser also
archives the committed source with its Git commit identity. The Homebrew formula builds that checksummed source on
macOS or Linux and installs `kubebao`, the `kubectl-kubebao` plugin symlink, and the plugin completion helper. The Krew
manifest references the four binary archives. The formula and Krew manifest record the
SHA-256 hashes of their respective archives.

GoReleaser release publication and Krew uploads remain disabled in the configuration. Formula generation, `make dist`,
and `make release-dist` write local files only. `make dist` replaces `dist/` and creates unsigned snapshot artifacts:

```sh
make release-check
make dist DIST_VERSION=0.0.0-dev
make verify-dist
make verify-homebrew
python3 scripts/stage_release.py dist
```

The staging command writes `dist/release/` once. It selects archives, SBOMs, build metadata, and package definitions.
It does not include unpacked build directories or rendered tool configuration. The staged `checksums.txt` covers all
payload files, including the source archive and `release-info.json`. A publishing run adds `provenance.sigstore.json`,
which attests those files and the checksum file. The provenance bundle is not included in its own checksums.
Staging refuses an existing output directory.

`release-info.json` binds the version and commit to the source archive, build kind (`snapshot` or `release`), and
Apple signing mode (`none` or `developer-id-notarized`). Verification and publication reject a mode that differs from
the requested build. Unsigned means no Developer ID signing or notarization from this release process. Checksums,
SBOMs, source builds, and GitHub provenance remain required for unsigned releases.

For snapshots, binaries can include uncommitted changes while the source archive always contains the committed
revision. Use a clean tagged checkout to qualify matching release artifacts.

Distribution tools and host archive hashes are pinned in `scripts/release-tool.sh`. Update a tool version and all four
hashes together after checking its upstream release. These tools are not linked into the CLI and do not change
`THIRD_PARTY_LICENSES`.

## Versioning and qualification

### Unsigned distribution rehearsal

The **Distribution rehearsal** workflow builds one checksummed snapshot payload and tests it natively on Linux and
macOS, on amd64 and arm64. It verifies the source identity, archive contents, SPDX SBOMs, executable startup, Krew
installation from the local archive, and a rebuild from the source archive. On macOS, it also builds and tests the
generated Homebrew formula in a temporary prefix.

The workflow runs when packaging files change and supports manual runs on a selected branch. It does not create tags,
publish releases, push packages, request Apple signing, or use the protected release environment. Keep publication
variables false and publication workflows disabled while using it.

### Release qualification

Use independent semantic version tags such as `v0.1.0` and `v0.1.0-rc.1`. Build metadata and numeric prerelease
identifiers with leading zeros are rejected. Prereleases never update default package channels or the latest stable
release pointer. Stable direct downloads remain available for version-pinned automation.

Before a release, record its CLI commit, shared-client version, operator versions and image/CRD identities, Kubernetes
versions, and exercised capabilities. Start from the validation levels in [compatibility](compatibility.md).
The release workflow checks executable startup, help, version output, and process results on all four native platforms.
It also rebuilds the source archive on those platforms and exercises the source formula on macOS in a temporary prefix.
The formula check does not install Homebrew's build dependencies or qualify bottles.
Those checks do not exercise an operator, prove HA behavior, or qualify production operation. Record command JSON and
support-bundle compatibility changes in the release notes.

## Prepare publication

Perform these steps only after public release is authorized:

1. Verify public repository visibility, enabled CI and security workflows, and successful checks on the selected source.
2. Enable immutable releases in GitHub repository settings. Publication fails if the setting cannot be read.
3. Configure a protected `release` environment with required reviewers, no self-review, and deployment restricted to
   `main`. The tagged Release workflow uses `release-rehearsal` without signing secrets for unsigned rehearsals.
4. Allow the pinned workflow actions under the repository's Actions policy. Keep publication restricted to trusted
   maintainers. Apple secrets are required only when Apple signing is selected.
5. Draft handwritten release notes in chat for review. After approval, commit them as `docs/releases/<tag>.md`, for
   example `docs/releases/v0.1.0.md`. The publishing job requires this tracked, nonempty file.
6. Enable Release Please and Release, then set `CLI_RELEASES_ENABLED=true` only after tagged publication is approved.
   Enable Publish Edge and set `CLI_EDGE_ENABLED=true` only after edge publication is approved. Leave
   `CLI_PACKAGES_ENABLED=false` until package promotion is approved separately. Edge also requires immutable releases;
   its publisher does not use Apple signing secrets or the stable release approval environment.

## Optional Apple signing

The `apple_signing` workflow input defaults to `false`, including publishing runs. Leave it disabled to distribute
without an Apple Developer account. It does not change the publication gate or repository-visibility requirements.

To sign a tagged local build, set `KUBEBAO_NOTARIZE=true` when running `make release-dist`. Snapshot builds reject that
setting. For workflow builds, select `apple_signing=true` and configure the following secrets in the protected `release`
environment. Signed rehearsals also use that environment.

Apple secrets for the opt-in path:

| Secret | Value |
| --- | --- |
| `MACOS_SIGN_P12` | Base64-encoded Developer ID Application certificate and private key in PKCS#12 format |
| `MACOS_SIGN_PASSWORD` | Password for the PKCS#12 file |
| `MACOS_NOTARY_ISSUER_ID` | App Store Connect API issuer ID |
| `MACOS_NOTARY_KEY_ID` | App Store Connect API key ID |
| `MACOS_NOTARY_KEY` | Base64-encoded App Store Connect API private key |

When selected, signing and notarization finish before macOS packaging. Native macOS jobs then verify the code signature
and Gatekeeper assessment. A signing failure stops the build; it never falls back to unsigned artifacts. With signing
disabled, those Apple-specific checks are skipped while artifact, source, formula, and runtime checks still run.

Unsigned macOS downloads can be blocked by Gatekeeper, depending on how they are downloaded and local policy. Use the
source-building Homebrew formula or a source build when that occurs. The implementation does not remove quarantine
or disable Gatekeeper. Local startup checks do not qualify every browser-download or managed-device policy path.
Apple credentials and hosted signing remain separately unqualified by unsigned tests.

## Run a rehearsal

Once Actions is enabled, create a reviewed version tag at the current `main` commit. Tags must identify a commit on
`main`, and that commit must match the workflow run's source SHA. This keeps the artifact provenance tied to the
compiled source. Tag pushes never publish.

Dispatch the Release workflow from `main`, select the existing tag, and leave `publish=false` and `apple_signing=false`.
It runs repository checks, builds a version suffixed with `-rehearsal`, verifies the artifacts, and executes them on four
native runners.
The payload remains a workflow artifact. No GitHub release, attestation, or package update is created. A rehearsal
does not use Apple signing secrets or establish signing qualification.

To qualify signing separately, select `apple_signing=true` with `publish=false`. This uses the clean tagged version and
Apple credentials, runs the Apple checks, and retains the payload as a workflow artifact. It still creates no release,
attestation, or package update.

No tag, workflow dispatch, or repository setting change is required for the local snapshot commands above.

## Publish approved artifacts

After the preparation and release approval are complete:

1. Confirm that the selected tag matches the `main` commit to dispatch. Review the committed release notes.
2. Let Release Please call Release for the merged version PR, or dispatch Release from `main` with the selected tag and
   `publish=true`. Select `release_please_draft=true` only to finalize the matching App-created empty draft. Leave
   `apple_signing=false` for an unsigned release, or select it after Apple signing qualification. Approve the protected
   environment requests.
3. Wait for source checks, artifact generation, content verification, source/formula builds, and native runtime jobs.
4. Inspect the resulting release URL, immutable state, version, commit, asset set, signing mode, and provenance bundle.

The publishing job downloads the verified payload and does not rebuild it. It generates build provenance, verifies
the workflow identity and source commit, and rechecks the remote tag. It creates or validates a draft with reviewed
notes, uploads the payload, and downloads the complete draft to compare every byte before publication. Stable releases
become latest; prereleases do not. Publication must return a confirmed immutable release.

An existing release causes preflight to fail unless matching empty Release Please draft finalization was requested.
Inspect a failed run and its draft before taking a recovery action. The workflow never replaces a published release,
accepts another author's draft, or overwrites assets.
After publication, correct defects with a new version. Reset `CLI_RELEASES_ENABLED=false` to close the publication gate.

For an edge download, verify the copied CI bundle with the Check workflow identity:

```sh
gh attestation verify checksums.txt --bundle provenance.sigstore.json \
  --repo kubebao/openbao-operator-cli \
  --signer-workflow kubebao/openbao-operator-cli/.github/workflows/check.yml \
  --source-ref refs/heads/main --source-digest <commit>
```

Replace `<commit>` with the full commit in `release-info.json`. Then verify the payload hashes with
`sha256sum -c checksums.txt` on Linux or `shasum -a 256 -c checksums.txt` on macOS. The separate gates mean that closing
tagged publication does not close edge publication; set `CLI_EDGE_ENABLED=false` to close edge.

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

The workflow downloads the published assets, verifies their checksums and provenance, and creates a Homebrew formula PR
and an upstream Krew index PR through the fork. Krew updates start from the current upstream default branch. Review
package installation and index requirements before merging either PR. Default Krew installation becomes available
only after upstream acceptance. Homebrew and Krew packaging are prepared locally; neither public installation path
is qualified before those checks.

The `channel` input selects `both`, `homebrew`, or `krew`. If only one channel completes, inspect its PR and retry the
other channel. Existing update branches are not force-pushed. Reset `CLI_PACKAGES_ENABLED=false` after promotion.
