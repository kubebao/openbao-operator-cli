# Diagnose workload and recovery observations

`kubebao doctor` collects selected Kubernetes evidence and explains workload readiness and recovery restrictions.

```sh
kubebao doctor prod -n security
kubebao doctor prod -n security --verbose
kubebao doctor prod -n security --output json
kubebao doctor prod -n security --fail-on error
```

The command accepts the same kubeconfig, context, namespace, API-group, and request-timeout options as
[status](status.md). It does not change cluster state, collect logs, read Secrets, or contact OpenBao or cloud APIs.
Bundle creation and `doctor --bundle` are not implemented.

## Evidence collection

The command first resolves the named `OpenBaoCluster` using the status collector. It then:

1. Lists StatefulSet candidates with `openbao.org/cluster=<name>` and verifies their controller reference against the
   cluster API version, kind, name, namespace, and UID. A conflicting reserved owner-UID annotation prevents association.
2. Lists Pod candidates with the same label and verifies their controller reference against a collected StatefulSet's
   name and UID. Pods controlled by other workload kinds are outside this initial workload scope.
3. Inspects restore requests in the selected namespace. It retains requests bound through `status.execution.targetUID`,
   `status.target.uid`, or a documented cluster-side restore origin or hold that identifies the request UID. A matching
   `spec.cluster` name alone does not prove association. Conflicting target identities remain unverified.
4. Reads the selected cluster again. A changed UID or generation makes diagnosis incomplete. Both read identities remain
   in the collection record, including the final generation when readable. Missing generation evidence also prevents
   verified target stability. A status-only update can change resourceVersion without invalidating the selected UID and
   generation.

Lists use pages of 100 candidates and inspect at most 1,000 candidates per resource kind. A candidate limit, repeated
continuation token, inconsistent list versions, duplicate UID, or invalid resource identity makes collection incomplete.
The limit includes unrelated restore requests inspected before filtering. Unrelated resources are not included in the
evidence. Ambiguous candidates produce metadata-only omissions with a reason, visible with `--verbose` or JSON output.

Legacy completed requests without target-UID bindings remain unassociated even when their target name matches. Retaining
such requests can make diagnosis incomplete; the CLI does not infer their ownership or current relevance from the name.

Denied or failed reads preserve independent evidence already collected. An absent restore API is recorded as not
applicable. A restore hold that cannot be correlated still limits the restore rule's conclusion. Collection occurs over
an interval and is not an atomic Kubernetes snapshot. Each retained resource records its identity, resourceVersion, and
collection time.

## Initial rules

The rule-set version is independent of the JSON schema version. Current rule-set version: `1`.

| Rule | Evidence and interpretation |
| --- | --- |
| `KBAO-CLUSTER-001` | Reported availability and its resource and condition generation acknowledgement |
| `KBAO-WORKLOAD-001` | Associated StatefulSet desired and Ready counts for its observed generation |
| `KBAO-POD-001` | Associated Pod population, Ready conditions, Failed phase, and reported container waiting reasons |
| `KBAO-RESTRICTION-001` | Positively declared pause, maintenance, restore holds, and active break glass |
| `KBAO-RESTORE-001` | UID-correlated restore progress and a held Failed or Unknown outcome |

Each finding includes a stable rule identifier, severity, description, resource and field references, suggested checks,
and any missing evidence that limits that rule. Errors appear before warnings and informational findings.

Pause, maintenance, and restore holds are informational restrictions rather than automatic failures. A historical restore
outcome without a matching current hold is not treated as a current recovery failure. The restriction rule reports
positively declared controls; absent optional configuration fields remain unknown in the evidence and do not establish
that a control is disabled.

Missing readiness fields and stale generation acknowledgement prevent complete evaluation of the affected readiness
rule. A condition transition time does not establish recent reconciliation. Reported readiness is not an independent
OpenBao health probe or a quorum check. No operator release has been qualified.

Tenant provisioning, backup and upgrade Jobs, TLS and networking prerequisites, PVCs, Services, and Events are outside
this initial rule set. The command makes no claim to validate those paths. Maintenance annotation observations do not
constitute verified propagation.

## Permissions and data handling

In addition to status's named-cluster `get` and discovery access, doctor requires namespace-scoped `list` on:

- `apps/v1` StatefulSets.
- `v1` Pods.
- `OpenBaoRestore` resources in the selected operator API group.

The command does not grant these permissions. It requires no Pod log or exec access. Field projections exclude workload
configuration, literal environment values, container arguments, arbitrary annotations, restore source objects,
authentication references, and break-glass nonces. Approved diagnostic text receives additional credential-pattern
redaction. Resource names and diagnostic messages can still require review before sharing.

## JSON and exit codes

JSON output uses `schemaVersion: "v1"` and contains `ruleSetVersion`, `analysisTime`, `failureThreshold`, `resultCode`,
`exitCode`, `findings`, `coverage`, and `evidence`. The evidence contains the shared snapshot, related projections,
omissions, and collection outcomes. References identify resources and fields within that evidence.

Coverage is `evaluated`, `not_applicable`, or `insufficient_evidence` for each rule. The analysis reference time is the
capture's completion time. Rules make no network requests. Serializing and reading equivalent evidence preserves the
analysis result, which provides the foundation for later offline bundle analysis.

`--fail-on` selects the lowest severity that returns exit code `2`: `info`, `warning` (default), or `error`. `none` disables
finding-based failure while preserving execution and incomplete-diagnosis failures.

- `0`: collection and applicable rules completed without a finding at the selected failure threshold.
- `1`: invalid input, configuration or output failure, missing target, or ambiguous API-group selection.
- `2`: one or more findings meet the selected threshold.
- `3`: collection or applicable rule evaluation is incomplete.

Incomplete diagnosis takes precedence over findings. Confirmed findings remain in the report when other evidence is
missing. Standard output contains the report; a nonzero result also writes a short result message to standard error.
