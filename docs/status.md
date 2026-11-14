# Observe one OpenBao cluster

`kubebao status NAME` reads the named `OpenBaoCluster` and its API discovery endpoint. It reports approved public fields
without contacting OpenBao or changing cluster state.

```sh
kubebao status prod -n security
kubebao status prod --context production --verbose
kubebao status prod --output json
kubebao status prod --watch --watch-interval 2s
```

Kubeconfig loading, context and namespace selection, credential plugins, rendering, and process results belong to the
CLI. The shared client owns projections, bounded reads, generation acknowledgement, and diagnostic interpretation.
The supported API adapters are `kubebao.org/v1alpha1` and
`openbao.org/v1alpha1`. API discovery does not qualify an operator release. Automatic
selection discovers both declared APIs and selects only an unambiguous served cluster API. If both APIs are served,
use `--api-group kubebao.org` or `--api-group openbao.org`. Explicit selection queries only that discovery endpoint.
Denied, malformed, or failed discovery remains incomplete and does not select another group.

## Evidence

The default view includes versions, replicas, availability, independent resource and condition acknowledgement,
restrictions, and reported operational facts. Attention entries come from selected shared rules; status does not fail
because a finding exists or a rule lacks evidence. `--verbose` adds structured reasons, field references, transition
times, and interpretation limits. Provider messages are excluded from text, verbose, and JSON output. Message fields
remain empty for layout compatibility. A separate local-text projection policy is not implemented.

Text output starts with a verdict derived from the attention entries: no issues reported, needs attention, or checks that
lack evidence. It never reports health that the operator did not report. Values follow in Cluster, Controls, and
Operations sections. The default view omits optional fields that the operator does not report, such as an absent
restore hold or operation lock; `--verbose` shows every field, a conditions table, and the collection steps.
Timestamps show their age relative to collection time.

Normalized evidence distinguishes missing values from malformed values. The inspected adapter can interpret documented zero
omission for ready replicas after current resource acknowledgement. An omitted backup failure counter remains unknown.
A matching resource generation does not acknowledge every condition. Missing, stale, and inconsistent condition
acknowledgement cannot establish a current finding. Collection time and transition time are separate; neither proves a
recent operator heartbeat. Service-registration labels are not independent health probes.

Status reads no Pods, StatefulSets, Jobs, Events, or restore requests. Maintenance is requested configuration; annotation
propagation stays `not_collected`. A restore hold is a restriction. Legacy `status.restore` fields do not establish
current restore progress.

## Watch and limits

Watch mode polls the selected UID and API group without rediscovery. Deletion, replacement, or failed collection stops
the command. A replacement is identified by the collection outcome; its status is not returned as the selected resource.
Each JSON line is a complete command report. Polling can miss intermediate states and is not event history. Ctrl+C stops
a watch without changing cluster state. The default interval is two seconds; the minimum is `100ms`.
On a terminal, text watch output redraws in place under a header with the interval and refresh time. Piped text and JSON
output append one complete report per collection.

One attempt has a 30-second deadline, configurable with `--request-timeout`. Each response is bounded to 8 MiB.
Redirects are rejected. Reads do not retry or fall back to another API group. Provider response bodies and errors do not
enter output.

## JSON and process results

The command envelope retains `schemaVersion: "v1"`, `resultCode`, `exitCode`, collection times, `target`, `observation`,
and `collection`. It includes `normalizedEvidence`, a shared capture containing approved typed facts and
actual collection outcomes. Display projections are mappings; diagnostic rules do not reinterpret them. Process exit
codes exist only in command envelopes, outside normalized evidence and pure analysis.

Collection steps retain the legacy `collected` label for successful reads. Bounded failures use outcomes such as
`denied`, `timeout`, `cancelled`, `invalid`, `partial`, `limit_reached`, and `repeated_continue_token`. HTTP 401 and 403 both
map to normalized `denied`. Missing targets and unavailable APIs remain distinct. Normalized collection records use
the shared library's outcome names and retain scope and interval details.

- `0`: collection completed, including unhealthy or uncertain operator state.
- `1`: invalid input, configuration or output failure, missing target, ambiguous discovery, or unsupported API-group selection.
- `3`: collection is incomplete, the selected UID disappeared or changed, or a resource cannot be projected.

Reports go to standard output. A nonzero result also writes a short result message to standard error; exit code `1`
prefixes it with `Error:`, and exit code `3` does not, because the report already describes the result. Input and
kubeconfig failures produce no status report. Version output has a separate JSON contract.

## Permissions

Status requires named-resource `get` on `openbaoclusters` in the selected API group and namespace, plus access to its
`/apis/<group>/v1alpha1` endpoint. Automatic selection queries both declared discovery endpoints. It requires no
`list`, `watch`, Pod log or exec, Secret access, roles, port forwarding,
or OpenBao/cloud API access. Watch mode uses the same permissions as one read.

When normalized input fails validation, JSON includes an optional `validationPath`, and text identifies that field.
The rejected normalized capture is omitted. Source values and provider errors are not included in the validation message.
HTTP 410 and 429 remain distinct shared `expired` and `rate_limited` outcomes; they produce incomplete status results.
