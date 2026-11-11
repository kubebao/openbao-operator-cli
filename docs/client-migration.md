# Shared client migration qualification

The CLI pins `github.com/kubebao/openbao-operator-client` at `v0.0.0-20261111115206-1edeecfd35ae`, client commit
`1edeecfd35aedcfa88af9fc57bf1d60dca765c3c`. This is the common milestone 6 candidate, not a release tag.
Milestone 5 qualified client `74412f30a4c0430092dfd603de15435260e7e86e`; the new candidate changes only client documentation.
The library uses evidence schema 2 and rule-set version 7. The CLI keeps
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

## Bounded operator integration

The qualified executable reports CLI commit `8cb99104e44974fd6bd8311f818cd9d7a1f36bfd`. A disposable `cli-client-m5` kind
cluster runs Kubernetes 1.36.1 on Linux/arm64 with operator/chart 0.6.1. The first attempt stopped when OrbStack's Docker
socket disappeared. Testing resumed after Docker returned, without restarting the shared service.

| Installed artifact | Identity |
| --- | --- |
| Operator chart | `ghcr.io/kubebao/charts/openbao-operator:0.6.1`, digest `sha256:03bc763ade61590f3616bff2738b379453554084cee12aa61035503c3d9fc338` |
| Running operator image | `ghcr.io/kubebao/openbao-operator@sha256:8a73e5a7c0f13d6786e7ca6d65af20cb36620d4464dc07a6e8210e59d17bcb97` |
| Cluster CRD spec SHA-256 | `7c3659f144c375092d7d1629d65004c15cc30ec41dd81e04a400842ada82ed14` |
| Restore CRD spec SHA-256 | `d3c781479988c032e00e45c6ec4c29f66aba1d11413f65f11f98dd0901b0ca64` |

CRD hashes cover sorted, compact JSON of each installed `spec`. Commands use ten-minute ServiceAccount tokens in namespace
`cli-sample`. The status reader can GET only the named cluster and cannot LIST Pods. The doctor reader adds namespace
StatefulSet, Pod, and restore LIST permissions. Authorization checks deny Secret GET, cluster PATCH, and cluster LIST.

Status exits zero after discovery and a named read. Watch retains the UID, omits rediscovery on the second poll, and exits
zero after SIGINT. Doctor with status-only credentials reports denied related reads, exits three, and retains the
independent cluster warning. Doctor with full reader permissions collects one StatefulSet, one Pod, and an empty restore
list, then verifies the original cluster UID and generation. All collection steps complete. The Pending Pod lacks required
readiness evidence, so the Pod rule remains incomplete and doctor exits three while retaining cluster and workload
warnings. This result does not qualify healthy-cluster diagnosis, nonempty restore correlation, lifecycle operations,
backup/upgrade execution, or another operator version.

Approved JSON reports and summaries remain under ignored `.local/milestone-5`. The disposable cluster is deleted and
scoped credential files are removed. Existing environments are preserved. Console PR #24 stays private, open, and
deferred. A common immutable client `v0` tag and both-consumer qualification remain milestone 6.
