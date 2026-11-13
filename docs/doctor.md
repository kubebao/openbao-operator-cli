# Diagnose operator and workload observations

`kubebao doctor` collects Kubernetes evidence and maps shared diagnostic results into CLI findings.

```sh
kubebao doctor prod -n security
kubebao doctor prod -n security --verbose
kubebao doctor prod -n security --output json
kubebao doctor prod -n security --fail-on error
kubebao doctor prod -n security --max-backup-age 48h
kubebao doctor --bundle prod-support.tar.gz
```

The command accepts the [status](status.md) connection options and its native and legacy API adapters.
It does not mutate resources, collect logs, read Secrets, or contact OpenBao or cloud APIs. Local provider-text
projection is not implemented. [Support bundles](support-bundles.md) provide offline diagnosis.

## Reads and association

The CLI resolves the named cluster with status collection, then declares these shared reads:

1. List StatefulSets in the selected namespace with `openbao.org/cluster=<name>`.
2. List Pods in that namespace with the same selector.
3. List snapshot restore candidates in that namespace without a selector.
4. GET the original named cluster again with the selected UID pinned.

The shared client verifies controller references and reserved UID metadata. Names and labels identify candidates only.
Pod membership requires the exact StatefulSet UID and a confirmed parent chain. A proven foreign chain is excluded from
target findings and population; conflicting or unconfirmed candidates limit coverage. Restore binding requires target
UID, execution target UID, or a request-UID-bound restore origin. A hold, operation ID, target name, or creation time alone
does not prove ownership. Native recovery is unsupported. A migrated object is a separate group and UID; migration
annotations do not establish cross-group identity or restore association.

Missing, changed, deleted, replaced, or failed final target verification prevents current related-resource findings.
The original identity remains selected. A status-only resourceVersion change does not invalidate matching positive UID
and metadata generation. Verification does not establish an atomic snapshot.

Each kind allows 1,000 candidates, 100 per page, 16 requests, 16 MiB transferred and retained, and 8 MiB per response.
The related sequence shares a 64-request, 3,000-candidate, 48 MiB transfer/retention budget and reserves a final
verification request. The entire command shares the requested deadline, including the initial cluster read. A limit,
pagination loop, invalid envelope, inconsistent list version, or duplicate identity prevents complete collection.
Independent approved evidence remains available after another scope fails.

## Rules and completeness

The pinned client release defines diagnostic behavior. Rule IDs remain stable; policies select rules, thresholds, and
the analysis reference time. No separate rule-set version is required.

| Rule | Selected interpretation |
| --- | --- |
| `KBAO-CLUSTER-001` | Current resource and Available-condition acknowledgement |
| `KBAO-CLUSTER-002` | Current-generation sealed-state observation |
| `KBAO-GENERATION-001` | Stale or inconsistent operator generation acknowledgement |
| `KBAO-CONDITION-001` | Recognized adverse operator conditions, including integration conditions |
| `KBAO-CONTROLLER-001` | Recorded workload or administrative controller errors |
| `KBAO-BACKUP-001` | Recorded consecutive backup failures |
| `KBAO-BACKUP-002` | Configured backups with no recorded successful completion |
| `KBAO-BACKUP-003` | Backup age exceeds `--max-backup-age`; excluded when the option is omitted |
| `KBAO-UPGRADE-001` | Recorded rolling-upgrade failure |
| `KBAO-WORKLOAD-001` | UID-associated StatefulSet readiness and its generation |
| `KBAO-POD-001` | UID-associated Pod phase, condition acknowledgement, readiness, and population |
| `KBAO-RESTRICTION-001` | Positively reported pause, maintenance, restore holds, and active break glass |
| `KBAO-RESTORE-001` | UID-confirmed restore progress and a correlated held failure or unknown outcome |

Findings have canonical rule IDs, subject-specific stable IDs, references, structured severities, suggested checks, and
coverage limits. Library critical and error severities map to CLI `error`; warning and info retain their names. Separate
Pods and restore requests retain separate identities. Summaries come from the shared rule implementation.

The CLI policy excludes historical restore failures without a current correlated hold. In-progress confirmed requests
can produce informational findings. Pause, maintenance, and holds remain independent restrictions. Missing optional
controls do not prove that they are disabled. An unavailable restore API is incomplete evidence, not proof that restore
analysis is inapplicable. Pod condition acknowledgement is independent of cluster or StatefulSet acknowledgement.

A uniquely held disposable target can produce an informational restore finding while its cleanup retention period is
active, even when the reported phase and execution stage remain `Unknown`. This requires complete reads, confirmed UID
association, reported target application, a recorded successful execution result, and a valid positive retention period.
The CLI preserves the reported phase. This finding does not establish completed cleanup or end-to-end recovery.

Coverage is `evaluated`, `not_applicable`, or `insufficient_evidence` for the selected rules. The policy receives the
capture completion time as its analysis reference. Missing evidence can prevent complete evaluation while independent
confirmed findings remain visible. Integration-condition findings describe operator observations. The selected rules do
not independently validate tenant provisioning, backup/upgrade Jobs, TLS, networking, storage health, Events,
maintenance propagation, OpenBao transactions, or quorum safety.

Backup-age evaluation requires a positive duration such as `--max-backup-age 48h`. The CLI does not select the age rule
when this option is omitted. The policy still records the shared library's unused default duration in that case.
Configuration does not prove that backup storage or provider permissions work. Missing failure counters remain unknown.

## Output and process results

JSON contains the command envelope fields `schemaVersion`, `analysisTime`, `failureThreshold`,
`resultCode`, `exitCode`, `findings`, `coverage`, `evidence`, the selected shared `policy`, and each finding `id`.
The evidence envelope adds `normalizedEvidence`, containing every admitted candidate before association filtering,
including foreign candidates. These bounded, approved facts preserve collection counts and deterministic replay.
The legacy `related` view contains confirmed associations and metadata-only omissions. Provider messages and raw
resource configuration are excluded from both views.

Normalized evidence and policy can be serialized and evaluated with the shared analyzer without network access. Display
fields and command exit codes are not analysis inputs. The CLI keeps thresholds and process decisions in command
wrappers; the library and pure analysis report contain no command exit code.

`--fail-on` selects the lowest severity that fails the command: `info`, `warning` (default), or `error`. `none` disables
finding-based failure while retaining collection and evaluation failures.

- `0`: collection and selected analysis complete without a finding at the threshold.
- `1`: invalid input, configuration/output failure, or missing target.
- `2`: a confirmed finding meets the threshold.
- `3`: collection or selected evaluation is incomplete, including target verification failure.

Incomplete diagnosis takes precedence over findings. Reports go to standard output; nonzero process results also write
a short message to standard error. Resource names and approved operational metadata remain in the evidence.

Offline results add `source: "bundle"` and retain the captured interval and analysis reference time. Live results use
`source: "live"`. Offline analysis uses the recorded policy and rejects Kubernetes options and `--max-backup-age`.
`--fail-on` and output options remain available. Legacy live display projections are absent from offline reports;
normalized evidence and diagnostic references remain available.

## Permissions

Doctor needs status's named-cluster GET and discovery access, plus namespace LIST on `apps/v1` StatefulSets, core `v1`
Pods, and restores in the selected API group. It grants no permissions and reads no Pod logs, exec sessions, Secrets,
container arguments, literal environment values, arbitrary annotations, restore source payloads, or authentication
references. No existing environment is required for the synthetic command tests.

## Validation and collection boundaries

A valid single non-StatefulSet controller reference excludes backup Job Pods from StatefulSet findings and population.
A contradictory known StatefulSet UID still limits coverage. Completed reads remain complete when associations are
unconfirmed; diagnostic completeness and exit-code precedence remain separate. Coverage retains every observed shared
insufficiency cause.

When normalized input fails validation, JSON includes an optional `validationPath`, and text identifies that field.
Rejected normalized captures are omitted. The field path contains no source values or provider errors. Kubernetes 1.34
and later are the qualification target; absent Ready-condition acknowledgement still prevents complete Pod evaluation.

## Read-only restore qualification

`TestOperatorRestoreDoctorAndBundleReplay` observes a caller-provisioned restore through live diagnosis, support-bundle
collection, and repeated offline diagnosis. It creates no Kubernetes resources. Run it after the fixture reaches a stable
reported phase. Ordinary `make check` skips this test; a skipped test adds no operator qualification.

Set these environment variables with the prefix `KUBEBAO_TEST_RESTORE_`:

| Suffix | Required value |
| --- | --- |
| `KUBECONFIG` | Dedicated kubeconfig path |
| `CONTEXT` | Context in that kubeconfig |
| `API_GROUP` | `kubebao.org` or `openbao.org` |
| `NAMESPACE` | Namespace containing the cluster and restore request |
| `CLUSTER`, `TARGET_UID` | Cluster name and expected current UID |
| `REQUEST`, `REQUEST_UID` | Restore request name and UID |
| `PHASE` | Expected reported restore phase |
| `CANDIDATE_COUNT` | Exact count of all restore requests in the namespace, from 1 to 1,000 |
| `COVERAGE` | Expected restore-rule state: `evaluated` or `insufficient_evidence` |
| `MISSING_REASON` | Required when coverage is insufficient; for example, `association_unconfirmed` |
| `FINDING_SEVERITY` | Expected finding for that request: `none`, `info`, or `error` |
| `EXIT_CODE` | Expected diagnosis exit code with `--fail-on none`: `0` or `3` |

Run the test with workspace resolution disabled:

```sh
GOWORK=off go test -count=1 -timeout=3m -v ./internal/cli \
  -run '^TestOperatorRestoreDoctorAndBundleReplay$'
```

The test verifies the declared target UID, request UID, phase, UID association, and final target verification. It retains
every admitted restore candidate, including requests without confirmed associations. Older unconfirmed requests can
therefore keep coverage insufficient. The test checks the declared coverage and exit code without changing the CLI's
historical-restore policy. Offline diagnosis uses the bundle's captured policy and reference time.
