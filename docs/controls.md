# Set cluster controls

Pause and maintenance change independent operator configuration fields. Each command reads one named
`OpenBaoCluster`, then submits one JSON Patch guarded by its UID, resource version, and generation.

```sh
kubebao pause prod -n security
kubebao resume prod -n security --wait
kubebao maintenance enable prod -n security --wait
kubebao maintenance disable prod -n security --dry-run --output json
```

The commands accept the [status connection options](status.md). Select `--api-group kubebao.org` or
`--api-group openbao.org` when both APIs are served. Selection remains bound to that group and UID through submission
and observation. A migrated object with a different UID or API group is a different target.

## Submission

`pause` sets only `spec.paused=true`; `resume` sets it to `false`. Maintenance enable and disable set only
`spec.maintenance.enabled`. An absent maintenance object is created with that field. Other configuration, maintenance
settings, recovery holds, and annotations remain unchanged.

The API server applies ordinary authorization and admission. A concurrent change can fail a JSON Patch precondition.
HTTP 422 is reported as `invalid`: its status alone cannot distinguish validation rejection from a failed patch test.
The command never refreshes a stale plan and resubmits it.

`--dry-run` sends `dryRun=All` to the API server. `dry_run_accepted` describes a hypothetical result and does not establish
that configuration persisted. It cannot be combined with `--wait`.

`accepted` means the API returned the requested value for the bound target. It does not establish that the operator
observed the change. `unknown` means the response cannot establish whether the request persisted. Read the selected
target before deciding on another action. The CLI never retries a mutation, rolls it back, or switches API groups.
With `--wait`, an unknown submission can be followed by read-only assessment. Its original `unknown` classification
remains in the submission result even if later evidence reports the requested state.

## Observation

`--wait` collects later evidence for up to two minutes. Configure the duration with `--wait-timeout` and the two-second
poll delay with `--poll-interval` (minimum `100ms`). Each collection and submission has its own 30-second
`--request-timeout`; the remaining wait deadline also bounds observation requests. Cancellation stops observation and
does not undo a submitted change.

Pause observation needs current resource and `Available` condition acknowledgement. Pausing needs
`Available=Unknown` with reason `Paused`. Resuming needs an evaluated condition with another reported reason. A resumed
cluster can remain unhealthy. Pause acknowledgement does not prove that already-started Jobs or OpenBao processes stop.

Maintenance observation adds namespace StatefulSet and Pod LISTs with `openbao.org/cluster=<name>`, then verifies the
original cluster again. The shared client confirms UID ancestry and checks maintenance annotations on every current
workload. A complete stable voter pool and any configured read-replica pool are required, including a configured
zero-replica read pool. Settled blue/green topology requires consistent `Idle` and stable-revision evidence. Active
transitions, incomplete reads, or unknown associations prevent verified propagation. Restore requests are not read.

A maintenance change can be accepted while reconciliation is paused. The command reports
`maintenance_propagation_blocked_paused`; a wait reports blocked observation. It never resumes the cluster. To apply
both controls, enable and verify maintenance before pausing. Resume and disable maintenance are separate actions.
Workload reads are skipped when pause evidence already prevents propagation assessment.

## Output and permissions

Text and JSON distinguish submission from assessment. JSON uses command schema `v1` with `target`, `plan`, `submission`,
optional `assessment` and `assessmentPolicy`, `collection`, `warnings`, `resultCode`, and `exitCode`. Assessment policy
records the analysis reference time and bounds. Plans contain approved identity and
concurrency fields. Provider messages, raw Kubernetes resources, transport errors, and credentials are excluded.
The assessment separates requested configuration from operator observation. Observed state does not attribute a
change to this request or prove that another operation has stopped.

- `0`: API acceptance, server dry-run acceptance, or requested observation when `--wait` is used.
- `1`: invalid input, configuration or output failure, ambiguous API selection, missing target, or API rejection.
- `3`: incomplete collection, unsafe evidence, unknown submission without verified later state, blocked observation,
  replacement or deletion, cancellation, or an expired wait.

Controls require named-resource `get` and `patch` on `openbaoclusters` in the selected API group and namespace, plus
access to its discovery endpoint. Automatic selection reads both declared discovery endpoints and requires an
unambiguous result. Maintenance waits also need namespace `list` on StatefulSets and Pods. Pause and resume waits need
only the named cluster read. No Secret, Pod log or exec, OpenBao, or cloud API access is added.

The shared client owns public projections, guarded submission, and pure assessments. The CLI owns credentials, API
selection, polling, output, and exit codes. See [client integration](client-migration.md) for qualification boundaries.
