# Shared client migration qualification

The CLI pins `github.com/kubebao/openbao-operator-client` at `v0.0.0-20261111112413-74412f30a4c0`, client commit
`74412f30a4c0430092dfd603de15435260e7e86e`. The library uses evidence schema 2 and rule-set version 7. The CLI keeps
command schema `v1` through presentation mappings. No local replacement or workspace is required for final builds.
Go 1.27.4 is required.

## Ownership and command boundaries

| Concern | Implementation |
| --- | --- |
| Public field projection, generation semantics, UID association, bounded reads, pure rules | Shared client |
| Kubeconfig, context, namespace, credential plugins, selected target, watch loop | CLI |
| Command JSON/text, thresholds, process results | CLI wrappers |
| Display projections and pure finding mapping | CLI packages without Kubernetes clients |

Status performs discovery and one named GET. After selection, watch polls the same group and UID without rediscovery.
Doctor declares bounded namespace StatefulSet, Pod, and restore LISTs, then a pinned final cluster GET. No Secret,
Pod log/exec, OpenBao request, mutation, or broader namespace discovery is added.

Migration fixtures identified a shared foreign-ancestry coverage gap. Client PR #17 fixes it in the library: a consistent
Pod → StatefulSet → other-cluster chain is foreign; conflicting annotations remain incomplete. A caller-supplied parent
assertion cannot override the workload metadata. No duplicate CLI ownership or diagnostic engine remains.

## Intentional corrections and output changes

- Only the inspected `openbao.org/v1alpha1` contract is supported. The scaffold's speculative `kubebao.org` schema alias is
  rejected before loading credentials.
- Provider messages are excluded from all formats. Legacy message fields remain empty. Structured reasons and references
  remain available; no local rich-text policy is implemented.
- Status attention and doctor findings use shared rule semantics. Resource and condition acknowledgement stay separate;
  Pod conditions also need their own acknowledgement. Missing, stale, invalid, and inconsistent evidence limit coverage.
- The inspected adapter can interpret documented ready-count zero omission. Unknown backup counters remain unknown.
- A restore hold alone is a restriction, not association proof. Target/execution UID or a request-bound restore origin is
  required. Unsupported restore reads prevent complete evaluation rather than proving inapplicability.
- Replacement status is not returned as the selected identity. Watch keeps the original UID and reports replacement.
- JSON adds subject-specific finding IDs, the selected shared policy, and normalized evidence. The normalized capture
  retains admitted candidates before filtering, including foreign resources, to preserve actual collection counts and
  replay. The legacy related view retains confirmed associations and metadata-only omissions.
- Command envelopes retain process exit codes. Shared captures and pure reports do not contain command process results.
  HTTP 401/403 share the normalized `denied` outcome. Additional bounded failure outcomes remain distinguishable.

## Validation

Local checks run with `GOWORK=off` against the published client version:

```sh
make check
make vulncheck
make licenses
make check-licenses
make dist VERSION=milestone-5
```

Command tests cover text/JSON, thresholds and exit precedence, actual request paths/selectors, permissions, pagination and
candidate limits, generation changes, UID deletion/replacement, watch cancellation, timeouts, redirects, output failures,
and sensitive-data exclusion. Pure tests replay serialized normalized evidence with fixed policy and analysis time.
Archive verification checks all four platforms, checksums, executable/license contents, and the native executable.
The dependency graph removes unused dynamic/discovery client dependencies and regenerates dependency notices.
The generator excludes the CLI at a package boundary, so its name cannot hide the similarly prefixed client module.
An inclusion guard checks the client license, and adjacent upstream NOTICE files enter the generated notices.

The CLI repository remains private with GitHub Actions disabled. These are local results, not hosted CLI CI results.
Client PR #17 passes its exact-head hosted checks and its signed squash merge matches the tested preview tree.

## Uncompleted optional integration

A disposable `cli-client-m5` kind cluster uses Kubernetes 1.36.1 and operator/chart 0.6.1. The operator and a development
cluster resource were installed, but OrbStack's Docker socket disappeared before scoped reader credentials could be
issued. The CLI live collection check did not run and establishes no operator integration qualification.
The shared Docker service was not restarted and existing environments were not changed.

Disposal remains pending while Docker is unavailable. When it returns, remove only this disposable cluster:

```sh
kind delete cluster --name cli-client-m5 --kubeconfig .local/milestone-5/admin-kubeconfig
```

The kubeconfig and test state remain under ignored `.local/milestone-5`. Console PR #24 stays private, open, and deferred.
A common immutable client `v0` tag and both-consumer qualification remain milestone 6.
