# Set cluster controls

Pause, maintenance, and restart change separate operator configuration fields. Each command reads one named
`OpenBaoCluster`, then submits one JSON Patch guarded by its UID, resource version, and generation.

```sh
kubebao pause prod -n security
kubebao resume prod -n security --wait
kubebao maintenance enable prod -n security --wait
kubebao maintenance disable prod -n security --dry-run --output json
kubebao restart prod -n security --dry-run
kubebao restart prod -n security --wait --wait-timeout 5m
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
that configuration persisted. It cannot be combined with `--wait` or `--request-file`.

`accepted` means the API returned the requested value for the bound target. It does not establish that the operator
observed the change. `unknown` means the response cannot establish whether the request persisted. Read the selected
target before deciding on another action. The CLI never retries a mutation, rolls it back, or switches API groups.
With `--wait`, an unknown submission can be followed by read-only assessment. Its original `unknown` classification
remains in the submission result even if later evidence reports the requested state.

## Rolling restart

`restart` writes one new RFC 3339 timestamp to `spec.runtime.restartAt`. It preserves other runtime fields and creates
an absent or null runtime object when needed. The operator propagates the marker and manages the rollout. The CLI does
not delete Pods or change pause, maintenance, restore holds, or upgrade strategy.

Preparation requires current cluster acknowledgement and reported desired and accepted `RollingUpdate` strategy.
Pause, a restore hold, an operation lock, active or unconfirmed break glass, and Blue/Green state prevent submission.
A missing strategy or incomplete evidence also prevents submission. Maintenance is independent of pause; its value
remains unchanged.

With `--wait`, the CLI reads bounded namespace StatefulSet and Pod lists and verifies the original cluster again.
Observation requires UID-confirmed ownership, complete fresh reads, exact expected pool populations, the requested
marker on each StatefulSet template and live Pod, converged StatefulSet revisions and replica counts, and current
Running/Ready Pods. A configured zero read-replica pool requires its StatefulSet and no live Pods. A positive
`minReadySeconds` also requires the StatefulSet's available replica count to match its desired count.

The assessment supports ordinary `RollingUpdate` StatefulSets with an absent or zero partition. `OnDelete`, nonzero
partitions, Blue/Green topology, incomplete reads, or conflicting ownership cannot establish an observed restart.
In particular, a template marker alone does not establish a completed rollout.

JSON includes the typed restart `plan`, the original `submission`, and optional `assessment` and `assessmentPolicy`.
`configured` records the requested cluster marker; `operatorObserved` records current workload convergence. Text prints
these as the submission, "Marker configured", and "Rollout converged". An `unknown` submission remains `unknown` even when a later read
confirms convergence. The marker is generated once and is never refreshed or resubmitted during waiting.

Observed convergence does not attribute Pod replacement to this request or prove serving health, Raft quorum, or
end-to-end application availability. Operator runtime qualification uses a disposable local cluster; it does not
establish HA or production operation.

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

## Continue observation

Add `--request-file FILE` to a control command to save its original plan before submission. The command creates a
mode-0600 file and refuses to overwrite any destination. If saving fails, it submits nothing. The file contains the
approved target identity, concurrency fields, and requested value or restart marker. It contains no kubeconfig,
credentials, endpoint, provider message, or submission result. A saved file records intent; it does not prove that the
API request was sent or accepted. Treat the file as local operational metadata.

```sh
kubebao restart prod -n security --kubeconfig ./config --context production \
  --api-group kubebao.org --request-file prod-restart.json --wait --wait-timeout 5m

# After a timeout, interruption, or unknown submission outcome:
kubebao observe --request-file prod-restart.json --kubeconfig ./config --context production --wait
```

`observe` performs one assessment by default. `--wait` polls until observation succeeds, becomes blocked, or reaches
`--wait-timeout`. It reuses the original plan, UID, and restart marker. It never submits a PATCH, creates a new marker,
or resumes reconciliation. Namespace and API group come from the plan. Any supplied `--namespace` or `--api-group`
must match. Select the original kubeconfig cluster and endpoint through the normal connection options. A different
cluster origin is rejected before a target read. Replacement UIDs stop observation before workload reads.

The file must use `kubebao-control-request/v1`, contain exactly one validated control or restart plan, and be a regular
file of at most 32 KiB. Links, special files, duplicate keys, unknown fields, invalid intent, and trailing JSON are
rejected before kubeconfig loading. Full command reports and support bundles are not request files. Dry-run commands
cannot create request files. Without a saved original plan, inspect current status before deciding on another action.

Observation confirms the current requested configuration and operator or workload evidence. It cannot reconstruct
missed transitions, establish the original submission outcome, or attribute the current state to that request. It does
not change the saved file. JSON reports set `readOnly: true` and omit `submission`. Exit `0` means the requested state
is observed; exit `3` means it remains pending, blocked, or unconfirmed. Invalid input or a connection identity mismatch
returns exit `1`. The original mutation's unknown outcome remains unknown.

## Output and permissions

Text and JSON distinguish submission from assessment. Text starts with a verdict, explains the result in plain
language, and suggests a follow-up command for the same target. Copyable commands preserve the resolved kubeconfig
file precedence, exact context, namespace, and selected API group. Paths are absolute and arguments use POSIX shell
quoting. Multiple kubeconfig files use an `env KUBECONFIG=...` prefix. Unsafe or redacted local metadata suppresses the
command. Connection arguments remain process-local and are excluded from JSON and support bundles.

A maintenance change that is blocked by pause suggests an explicit `kubebao resume` and never performs it. While
`--wait` polls, a transient progress line on an interactive standard error shows the elapsed time and pending reasons.
It is not written to pipes or files. JSON uses command schema `v1` with `target`, `plan`, `submission`,
optional `assessment` and `assessmentPolicy`, `collection`, `warnings`, `resultCode`, and `exitCode`. Assessment policy
records the analysis reference time and bounds. Plans contain approved identity and
concurrency fields. Provider messages, raw Kubernetes resources, transport errors, and credentials are excluded.
The assessment separates requested configuration from operator observation. Observed state does not attribute a
change to this request or prove that another operation has stopped.

- `0`: API acceptance, server dry-run acceptance, or confirmed observation.
- `1`: invalid input, configuration or output failure, ambiguous API selection, missing target, or API rejection.
- `3`: incomplete collection, unsafe evidence, unknown submission without verified later state, blocked observation,
  replacement or deletion, cancellation, or an expired wait.

Controls require named-resource `get` and `patch` on `openbaoclusters` in the selected API group and namespace, plus
access to its discovery endpoint. Automatic selection reads both declared discovery endpoints and requires an
unambiguous result. Maintenance and restart waits also need namespace `list` on StatefulSets and Pods. Pause and resume waits need
only the named cluster read. `observe` requires only these reads; it does not require `patch`. Its saved API group
skips discovery. No Secret, Pod log or exec, OpenBao, or cloud API access is added.

The shared client owns public projections, guarded submission, and pure assessments. The CLI owns credentials, API
selection, polling, output, and exit codes. See [compatibility](compatibility.md) for validated versions and limits.
