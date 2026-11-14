# Development

## Repository layout

| Path | Contents |
| --- | --- |
| `cmd/kubebao` | Process entry point |
| `internal/cli` | Commands, output handling, and process result codes |
| `internal/buildinfo` | Version, commit, and runtime metadata |
| `internal/evidence` | Shared typed observations, references, and sanitization |
| `internal/collect` | Kubernetes access, ownership association, and approved projections |
| `internal/render` | Terminal presentation: colour detection, symbols, fields, tables, trees, and wording |
| `internal/status` | Human-readable status view |
| `internal/doctor` | Diagnostic policy, finding mapping, and the diagnosis view with its ownership tree |
| `internal/control` | Guarded control submission, bounded waits, and the control result view |
| `scripts` | Local distribution packaging and dependency license generation |
| `.github` | CI workflows and contribution templates |

## Commands

| Command | Result |
| --- | --- |
| `make install` | Downloads Go module dependencies |
| `make lint` | Runs pinned golangci-lint and configured formatting checks |
| `make lint-fix` | Applies configured formatters and supported lint fixes |
| `make test` | Runs Go tests with the race detector |
| `make check` | Runs lint, race tests, and the binary build |
| `make build` | Builds `bin/kubebao` with version and commit metadata |
| `make vulncheck` | Checks reachable Go and standard-library vulnerabilities |
| `make licenses` | Regenerates `THIRD_PARTY_LICENSES` for linked dependencies |
| `make check-licenses` | Checks the generated dependency notices for drift |
| `make dist` | Builds four snapshot archives, SPDX SBOMs, checksums, and package metadata |
| `make release-dist` | Builds artifacts from a clean version tag without publishing |
| `make release-check` | Checks GoReleaser configuration and release safety tests |
| `make verify-dist` | Verifies artifact contents and exercises the current host's archive |
| `make verify-homebrew` | Runs the source formula's build and functional tests in a temporary prefix |

The module requires Go 1.27.4. With `GOTOOLCHAIN=auto`, the Go command selects a compatible toolchain. The Makefile pins
golangci-lint to `v2.14.0`, govulncheck to `v1.9.0`, and go-licenses to `v2.0.1`.

Formatting uses gofumpt, goimports, and golines. The line limit is 140 characters. Cognitive complexity is limited to 30.
The CLI does not inherit the console's generated-code paths or blanket security-linter exclusions.

## Build metadata and packaging

By default, Make derives the version from `git describe` and the commit from Git. Override them for a named local build:

```sh
make build VERSION=0.0.0-dev COMMIT=local
./bin/kubebao version --output json
make dist DIST_VERSION=0.0.0-dev
make verify-dist
```

GoReleaser is the distribution build implementation. `scripts/release-tool.sh` downloads GoReleaser `2.20.2` and Syft
`1.57.1` into ignored `.local/release-tools` directories. Verification also uses Krew `0.5.0`. SHA-256 pins cover each
Linux/macOS amd64/arm64 tool archive.
The first invocation requires network access, `curl`, `tar`, and `shasum`. Packaging verification requires Python 3.

The platforms are `linux/amd64`, `linux/arm64`, `darwin/amd64`, and `darwin/arm64`. Each archive contains `kubebao`,
`LICENSE`, and `THIRD_PARTY_LICENSES` at its root. Each SPDX SBOM describes the compiled binary and records its hash.
`dist/checksums.txt` covers all four binary archives, all four SBOMs, the source archive, and `release-info.json`.
The latter records the build kind and Apple signing mode. Package metadata is generated at
`dist/homebrew/Formula/kubebao.rb` and `dist/krew/kubebao.yaml`. The formula builds the checksummed source archive and
embeds the release version and commit. Snapshot URLs describe proposed artifacts; no corresponding
release exists. `make dist` replaces `dist/` and never publishes.

`DIST_VERSION` must be a semantic prerelease version. Distribution builds embed the actual source commit; `COMMIT`
overrides apply only to `make build`. Snapshot binaries can include uncommitted changes. GoReleaser's source archive
always contains the committed revision, so dirty snapshots can have different binary and source content. Use a clean
tagged checkout for release qualification. GoReleaser publication and Krew uploads are disabled in `.goreleaser.yaml`;
formula generation does not update the Homebrew tap.

Tagged builds are also unsigned by default. Set `KUBEBAO_NOTARIZE=true` only when a tagged build must use the Apple
credentials described in [release procedures](releases.md). Invalid values are rejected. Snapshot builds reject signing.
Here, Apple signing means Developer ID signing and notarization; it does not describe Go's platform-specific code-signing
details. No path changes quarantine attributes or disables Gatekeeper.

`make verify-dist` checks checksums, archive entries, executable permissions, SBOM binary hashes and dependencies, and
package versions and archive references. It executes the current host's archived binary with an absent kubeconfig.
It checks standalone and plugin help, version JSON, and error exit codes. It installs the generated manifest and local
archive into a temporary Krew root. If kubectl is on `PATH`, it also checks real kubectl plugin forwarding through that
installation. Cross-compilation and these offline checks do not establish operator compatibility.

The verifier also checks the source archive's Git commit, entry bounds, required inputs, formula checksum reference,
and requested signing mode. It selects the compatible Go toolchain already selected by the repository, downloads
locked dependencies, then rebuilds and exercises the source archive with module proxy access disabled during compilation.

`make verify-homebrew` requires installed Homebrew 7 or newer and a compatible Go toolchain. It loads the generated
formula through Homebrew, runs its dependency-fetch and build methods, and runs its functional test block with a minimal
assertion harness. It uses a
unique temporary tap directory, prefix, logs, and trust configuration; it does not register a tap or install a host
package. This checks formula behavior with the selected Go toolchain, not Homebrew's dependency-installation or bottle
workflows. Set `DIST_DIR=payload` on the verification targets to check a downloaded payload.

## Continuous integration

GitHub Actions is currently disabled during initial development. Run the Make checks locally before pushing changes.

When Actions is enabled, the Check workflow runs on pull requests, pushes to `main`, and merge groups. It runs Go checks,
vulnerability analysis,
dependency notice verification, and four-platform archive builds and verification. Generated artifacts are retained as
workflow artifacts. Ordinary checks never publish.

The manual Release workflow defaults to an unsigned rehearsal. Public publication requires a separate repository gate,
immutable releases, and a protected environment. Apple signing is a separate opt-in. Its runtime matrix executes the
same archived binaries and rebuilds the source on all four platforms; macOS jobs also exercise the formula.
Package promotion has a separate gate and opens PRs without merging them. These workflows do
not run while Actions remains disabled. See [release procedures](releases.md).

Actions are pinned to commit SHAs. Dependabot proposes weekly Go and GitHub Actions updates.

CodeQL and dependency review run automatically for public repositories. For a private repository, an administrator must
enable the applicable GitHub Code Security features and then set these repository variables to `true`:

- `ENABLE_CODE_SCANNING` for CodeQL.
- `ENABLE_DEPENDENCY_REVIEW` for dependency review on pull requests.

Until enabled, those jobs are skipped. A skipped job is not a successful security analysis. Go security linting and
`make vulncheck` remain part of the Check workflow. See [GitHub security feature availability](https://docs.github.com/en/code-security/getting-started/github-security-features).

## Scaffold reference

The tooling and engineering conventions were adapted from `kubebao/openbao-operator-console` at commit
`529bf8ef70099aedd8c7fef3a5c6df943e7f00a4`. The scaffold excludes the console's uncommitted edge-publication work, frontend,
database, authentication, container, and Helm deployment tooling.


## Shared client development

Use Go 1.27.4 and the client version pinned in `go.mod`. Coordinated development can use an untracked `go.work` file.
Final qualification uses `GOWORK=off` and no local replacement. The public client dependency requires no private-module
credentials. Regenerate notices with `make licenses` after a dependency change.

Run `make check`, `make vulncheck`, `make check-licenses`, and `make dist` for the migration. Verify archive checksums and
contents. Synthetic command tests cover discovery/named-read boundaries, doctor selectors and limits, UID/generation
verification, interruption, timeout, redirects, output failure, process codes, and sensitive-data exclusion. Pure mapping
tests replay serialized normalized evidence with the same policy and reference time. Operator qualification must record
its image/CRD identities and exercised capabilities separately.

## Rolling-restart runtime qualification

Run `TestOperatorRestartAndRollout` only against a disposable local Kind fixture. It sends a server dry-run request,
submits one guarded restart, waits for observed convergence, and checks that every original UID-confirmed Pod was
replaced. The test creates no Kubernetes resources. It requires an existing healthy fixture with the native API,
RollingUpdate strategy, and get/patch permission on the selected cluster plus namespace StatefulSet and Pod lists.

Set all fixture identifiers and the mutation opt-in before running it:

```sh
KUBEBAO_TEST_RESTART_ALLOW_MUTATION=true \
KUBEBAO_TEST_RESTART_KUBECONFIG=/path/to/disposable-kubeconfig \
KUBEBAO_TEST_RESTART_CONTEXT=kind-restart-fixture \
KUBEBAO_TEST_RESTART_NAMESPACE=restart-tests \
KUBEBAO_TEST_RESTART_CLUSTER=bao \
KUBEBAO_TEST_RESTART_TARGET_UID='<exact-cluster-uid>' \
GOWORK=off go test -race -count=1 ./internal/cli -run '^TestOperatorRestartAndRollout$' -v
```

The harness rejects non-Kind context names, missing fixture identifiers, or a mismatched target UID before submission.
Its mutation opt-in is separate from ordinary `make check`. Use a separate fixture for each intended topology and
record the operator source, image, CRD, Kubernetes, client, and CLI identities with the results. The UID replacement
check belongs to this harness; command assessment reports current rollout convergence without attributing causality.
