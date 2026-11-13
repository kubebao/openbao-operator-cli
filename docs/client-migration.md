# Shared client integration

The CLI pins `github.com/kubebao/openbao-operator-client` through `go.mod`. Qualification identifies the exact client and
CLI candidate separately from an operator release. The library uses evidence schema 5 and rule-set version 9. The CLI keeps
command schema `v1` through presentation mappings. No local replacement or workspace is required for final builds.
Go 1.27.4 is required.

## Ownership and command boundaries

| Concern | Implementation |
| --- | --- |
| Public field projection, generation semantics, UID association, bounded reads, pure rules | Shared client |
| Kubeconfig, context, namespace, credential plugins, selected target, watch loop | CLI |
| Guarded control plans, submission, and pure assessment | Shared client |
| Command JSON/text, thresholds, process results, and bounded control waits | CLI wrappers |
| Display projections and pure finding mapping | CLI packages without Kubernetes clients |

Status performs discovery and one named GET. After selection, watch polls the same group and UID without rediscovery.
Doctor declares bounded namespace StatefulSet, Pod, and restore LISTs, then a pinned final cluster GET. Observation
adds no Secret, Pod log/exec, OpenBao request, mutation, or broader namespace discovery. Separate
[control commands](controls.md) submit guarded field-scoped PATCH requests and assess later evidence.

A consistent Pod → StatefulSet → other-cluster chain is foreign. Conflicting ancestry remains incomplete evidence.
A caller-supplied parent assertion cannot override workload metadata. The CLI uses the shared ownership and diagnostic
implementation.

## Intentional corrections and output changes

- Separate inspected contracts support `kubebao.org/v1alpha1@22894cdc/restore-retention-v1` and
  `openbao.org/v1alpha1@9671c306/restore-retention-v1`. Automatic discovery requires an unambiguous API; explicit selection
  reads only that group. Group and UID remain pinned. Migration annotations do not imply cross-group identity.
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

Run checks against the published client pin with workspace resolution disabled:

```sh
GOWORK=off make check
GOWORK=off make vulncheck
GOWORK=off make licenses
GOWORK=off make check-licenses
GOWORK=off make dist VERSION=client-qualified
```

Command tests cover text/JSON, API ambiguity and selection, request paths/selectors, permissions, pagination and candidate
limits, UID deletion/replacement, cancellation, timeouts, redirects, output failures, and sensitive-data exclusion.
Native command regressions cover status, diagnosis, support-bundle creation, and offline replay. Control tests verify one
PATCH, optimistic-concurrency fields, unknown outcomes without resubmission, bounded waits, maintenance while paused,
and workload-read failures. Captured policy fixes the reference time for deterministic offline analysis.

The dependency notice generator includes the client license and upstream NOTICE files. Final builds require no local
replacement or workspace. The CLI repository has GitHub Actions disabled; local checks do not establish hosted CLI CI.
Exact client, CLI, operator, image, and CRD identities belong in qualification pull requests and release notes.

## Operator qualification boundary

The inspected contracts cover legacy operator `0.6.1` and native candidate
`22894cdc0a4aa5c442a07f2bf9902fe28ce02ea1`. The native contract does not qualify a released native operator.
The shared client's [compatibility document](https://github.com/kubebao/openbao-operator-client/blob/f6a44c94d608ac12671a12ff13b6e2d84b202c1e/docs/compatibility.md)
describes the operator fixtures and validation limits. API discovery alone does not qualify another operator build.

Control qualification uses disposable Kubernetes 1.36.1 clusters on Linux/arm64 with OpenBao 2.7.2, self-initialization,
the Development profile, and enabled admission policies. Fixtures cover voter-only, positive and zero read pools,
and an initialized revisioned blue/green pool in `Idle`. API acceptance, current operator acknowledgement, and workload
annotations remain separate results. These fixtures do not qualify HA or production operation, a completed upgrade,
active blue/green transitions, backup execution, nonempty restore execution, or control behavior on Kubernetes 1.34.

The Development fixtures retain `UserAccessBootstrap=Unknown`. Doctor preserves incomplete condition coverage and exits
3; bundle creation completes and offline diagnosis reproduces that result. This does not qualify complete healthy-cluster
diagnosis for every selected rule.

Kubernetes 1.34 is the observation floor. Missing current Pod Ready-condition acknowledgement limits coverage; the CLI
never infers generation from omission. The shared client records its Kubernetes 1.34.11 field check separately.

## Nonempty restore observation boundary

Read-only CLI runtime qualification covers native operator build `587dd16386bc5a234520e22eec1bbedb904fa6dd` in a migrated
demo on Kubernetes `1.36.1+k3s1`, Linux/amd64, and legacy operator `0.6.1` on Kubernetes `1.36.1`, Linux/arm64. Both use
OpenBao `2.7.2`. Fixture harnesses independently verify selected restored KV data; the CLI reads Kubernetes observations.

Native coverage includes an unheld historical `Unknown` request and a fresh disposable target during cleanup retention.
Historical concerns remain excluded by CLI policy. Older requests without UID bindings keep restore coverage
insufficient. During confirmed cleanup retention, the CLI reports an informational finding and preserves the reported
`Unknown` phase and stage. All admitted namespace candidates remain in the capture. Independent adverse cluster
conditions can still make the overall diagnosis incomplete.

The legacy request reports `Unknown` with a matching hold and produces an error finding. Restore coverage is evaluated;
missing backup-counter evidence keeps the overall diagnosis incomplete. Both baselines cover support-bundle collection
and deterministic offline replay with the captured policy and reference time. Complete reads can produce a successful
bundle while diagnosis exits `3`.

A separate shared-client runtime test covers a terminal `Completed` receipt and absence of the disposable target. Live
CLI fixture coverage uses retained `Unknown` targets. These checks do not qualify other operator builds, every failure
mode, HA, production recovery, or the correctness of arbitrary restored application data.

## Incomplete and invalid evidence

Exactly one valid Job controller excludes a Pod from StatefulSet membership. Completed backup Pods do not limit the Pod
rule or satisfy its population count. Missing or multiple controllers, and controller kinds that conflict with a known
StatefulSet UID, remain incomplete evidence. This exclusion does not prove ownership by another cluster.

Completed reads remain complete when an association is unconfirmed. Doctor still returns exit 3 for incomplete rule
coverage, including when `--fail-on none` is selected. JSON preserves the completed collection result separately from
the incomplete diagnostic result. Coverage retains the shared insufficiency reasons.

Malformed normalized input adds `validationPath` to command JSON and a validation-field line to text output. The field
contains only library-owned names and indices. A rejected normalized capture is omitted from output. Provider errors
and invalid source values do not enter the diagnostic message.
