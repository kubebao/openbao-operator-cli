# Cluster status

`kubebao status` reports public configuration and status from one `OpenBaoCluster`. It requires no console or backend
service. It does not probe OpenBao or perform corrective actions.

```sh
kubebao status prod -n security
kubebao status prod -n security --verbose
kubebao status prod -n security --output json
kubebao status prod -n security --watch
```

## Select the target

Use `--kubeconfig` to select a file and `--context` to select a Kubernetes context. Otherwise, the command uses standard
client-go kubeconfig loading, including `KUBECONFIG`, and the current context. The namespace comes from `--namespace`
(`-n`), the context, or Kubernetes' default namespace. The CLI does not fall back to in-cluster credentials.

The CLI checks `openbao.org/v1alpha1` and `kubebao.org/v1alpha1` for the named cluster. If both contain a match, collection
stops with `ambiguous_api_group`. Select the intended API group:

```sh
kubebao status prod -n security --api-group openbao.org
```

A denied discovery or resource read remains distinct from an absent API or missing resource. If one group is readable
and the other cannot be checked, the command prints the available observation and returns incomplete collection.

No operator release has been qualified. The v1alpha1 adapter projects known fields reviewed against operator source,
including release `0.6.1`. API discovery does not establish release compatibility. Output identifies this interpretation
limit. Unsupported API versions are not interpreted.

## Read the output

Human-readable output leads with reported conditions and restrictions that need inspection, then shows:

- Context, Kubernetes cluster name, namespace, API group, and resource UID.
- Desired and observed OpenBao versions and voter and read-replica counts.
- Reported availability, phase, and generation acknowledgement.
- Requested pause and maintenance settings, operation locks, restore holds, and break-glass state.
- Reported backup outcomes, rolling and blue-green upgrade progress, and legacy restore restart fields.

Missing numeric and Boolean fields appear as unknown. In particular, omitted optional fields are not converted into
reported zero or false values. Absence of a status object does not prove that a capability is disabled or idle.

`--verbose` includes condition messages, evidence field references, transition times, and interpretation limits. JSON
always includes the projected conditions and messages. Known credential patterns receive additional redaction, but
diagnostic text can still contain sensitive information. Review output before sharing it.

Top-level and per-condition generation acknowledgement remain separate. An observation for a previous generation is
retained and identified. A matching generation does not establish when the operator last reconciled. Collection time is
the time of the Kubernetes read; condition transition times are not reconciliation heartbeats. OpenBao service-registration
conditions describe labels reported by OpenBao and are not independent health probes.

This version reads no Pods, StatefulSets, Jobs, Events, or `OpenBaoRestore` resources. It reports maintenance as requested
configuration and marks annotation propagation `not_collected`. The restore-hold annotation indicates a management
restriction; legacy `status.restore` fields do not establish current restore progress.

## Watch mode

`--watch` prints successive complete views. With `--output json`, each line contains one complete report. A consumer must
parse the output as newline-delimited JSON rather than as one JSON document.

Watch mode polls the selected resource every two seconds. Set `--watch-interval` to change the delay; the minimum is
`100ms`. Polling can miss intermediate states and is not an event history. It needs only named-resource `get` permission.

The first selection pins the API group and UID. Later reads do not rediscover or switch resources. Deletion or replacement
stops the command with exit code `3`; a replacement UID remains visible in the collection result. A collection failure also
stops the command. Press Ctrl+C to stop a running watch without changing cluster state.

Each collection attempt has a 30-second timeout. Change it with `--request-timeout`. Timeout and transport failures are
recorded as failed collection; provider error bodies are excluded.

## JSON and process results

Status JSON uses `schemaVersion: "v1"` independently of the Kubernetes API version. It contains:

| Field | Meaning |
| --- | --- |
| `resultCode`, `exitCode` | Stable collection result and process result |
| `startedAt`, `completedAt` | Collection attempt interval |
| `target` | Selected context and resource identity |
| `observation` | Approved projected evidence, or `null` if unavailable |
| `collection` | Discovery and read outcomes, collection times, and resource identities when observed |

Result codes are `completed`, `not_found`, `ambiguous_api_group`, `incomplete`, `invalid_resource`, `resource_deleted`, and
`resource_replaced`. Collection outcomes are `collected`, `not_applicable`, `not_found`, `forbidden`, `unauthorized`,
`unsupported`, and `failed`.

- `0`: collection completed. An unhealthy cluster can return this code.
- `1`: invalid input, configuration failure, missing target, ambiguous API group, or output failure.
- `3`: collection is incomplete, the selected resource changed identity or disappeared, or its fields cannot be projected.

When collection produces a report, it goes to standard output, including incomplete reports. A nonzero result also writes
a short error to standard error. Input and kubeconfig failures produce no status report. Version output has its own
command-specific JSON contract.

## Permissions

The command requires `get` on the named `openbaoclusters` resource and access to the selected API discovery endpoint.
Without `--api-group`, it checks both known endpoints. Ordinary Kubernetes discovery permissions commonly provide that
access. Watch mode uses the same permissions as a single read.

It does not require `list`, `watch`, `pods/log`, or Secret access. It does not create roles, exec into Pods, port-forward,
or call OpenBao or cloud APIs. Credential plugins configured in kubeconfig retain their normal behavior.
