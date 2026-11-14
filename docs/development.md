# Development

## Repository layout

| Path | Contents |
| --- | --- |
| `cmd/kubebao` | Process entry point |
| `internal/cli` | Commands, output handling, and process result codes |
| `internal/buildinfo` | Version, commit, and runtime metadata |
| `internal/evidence` | Shared typed observations, references, and sanitization |
| `internal/collect` | Kubernetes access, ownership association, and approved projections |
| `internal/status` | Human-readable and JSON status rendering |
| `internal/doctor` | Pure diagnostic rules, coverage results, and findings rendering |
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
| `make dist` | Builds four local distribution archives and SHA-256 checksums |

The module requires Go 1.27.4. With `GOTOOLCHAIN=auto`, the Go command selects a compatible toolchain. The Makefile pins
golangci-lint to `v2.14.0`, govulncheck to `v1.9.0`, and go-licenses to `v2.0.1`.

Formatting uses gofumpt, goimports, and golines. The line limit is 140 characters. Cognitive complexity is limited to 30.
The CLI does not inherit the console's generated-code paths or blanket security-linter exclusions.

## Build metadata and packaging

By default, Make derives the version from `git describe` and the commit from Git. Override them for a named local build:

```sh
make build VERSION=0.0.0-dev COMMIT=local
./bin/kubebao version --output json
make dist VERSION=0.0.0-dev COMMIT=local
```

The default platforms are `linux/amd64`, `linux/arm64`, `darwin/amd64`, and `darwin/arm64`. Set `DIST_PLATFORMS` to select
a subset and `DIST_DIR` to change the output directory. Each archive contains `kubebao`, `LICENSE`, and
`THIRD_PARTY_LICENSES`. `checksums.txt` covers the archives from that invocation. Cross-compilation establishes build
coverage; it does not establish runtime or operator compatibility qualification.

## Continuous integration

GitHub Actions is currently disabled during initial development. Run the Make checks locally before pushing changes.

When Actions is enabled, the Check workflow runs on pull requests, pushes to `main`, and merge groups. It runs Go checks,
vulnerability analysis,
dependency notice verification, and four-platform archive builds. Archives are retained as workflow artifacts. No
workflow publishes a release or deploys an environment.

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
