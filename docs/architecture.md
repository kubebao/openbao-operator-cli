# Architecture and scope

## Overview

The Go executable provides cluster status, workload and recovery diagnosis, support bundles, offline diagnosis,
guarded controls, local archive packaging, and development checks. Status reads one named `OpenBaoCluster` through the
caller's kubeconfig and projects public v1alpha1 fields into a typed observation. Separate API adapters support `kubebao.org` and
`openbao.org`, unambiguous API discovery, explicit selection, and UID-pinned polling.

`status` does not collect related workloads or restore requests. It reports the restore-hold
annotation and legacy cluster restore fields, with their limits identified. Maintenance annotation propagation and current
`OpenBaoRestore` progress remain uncollected by status. See [status](status.md).

`doctor` adds bounded collection of associated StatefulSets, Pods, and restore requests. Pure rules explain readiness and
recovery restrictions from that captured evidence. It does not change cluster state or collect logs. See [diagnosis](doctor.md).

## Safety model

- **Read-only by default.** `list`, `status`, `doctor`, and `support-bundle` only read. They never pause a cluster or
  enable maintenance to collect evidence.
- **Reported, not probed.** Values come from the operator's status and from Kubernetes resources. `kubebao` does not
  probe OpenBao, so a report describes what the operator observed, not live serving health.
- **Unknown stays unknown.** Evidence that could not be read, or that the operator does not report, is shown as unknown.
  It never counts as healthy.
- **Ownership by UID.** Related StatefulSets, Pods, and restores are matched through owner references and UIDs, never
  by name or label alone.
- **Guarded controls.** Each control changes one field and is bound to the cluster's UID, resource version, and
  generation, so it fails if the cluster changed since it was read. A request with an unknown outcome is never retried.
  API acceptance and operator observation are reported separately; `--wait` waits for the operator.
- **Independent controls.** Pause and maintenance are separate. Maintenance never resumes a paused cluster.
- **Approved fields only.** Secrets, logs, provider messages, credentials, and arbitrary annotations are not collected.
  Review resource names in a support bundle before you share it.

## Scope

`kubebao` connects to Kubernetes through the caller's kubeconfig. It requires neither the console nor a backend service.
The operator retains responsibility for reconciliation and lifecycle safety.

The command set includes cluster listing, status, diagnosis, local support bundles, offline diagnosis, pause and resume, maintenance
enable and disable, and guarded rolling restart. Backup execution, upgrades, restores, scaling, installation, and automatic repair remain
outside its scope. General OpenBao administration continues through `bao`.

The implementation separates:

- Kubernetes discovery, resource association, and collection.
- Version-specific adapters into normalized, sanitized observations.
- Diagnostic rules that consume observations without network access.
- Bundle reading and writing.
- Guarded control requests and their verification predicates.
- Human-readable and versioned JSON rendering.

`github.com/kubebao/openbao-operator-client` owns typed evidence, approved public API projection, bounded collection,
UID associations, and pure diagnostic rules. `internal/collect` configures the caller's kubeconfig transport, declares
command scopes, and maps shared facts to CLI presentation. `internal/evidence` holds display models and the typed
normalized capture; `internal/doctor` selects policy and maps shared findings. Neither package imports Kubernetes clients.
`internal/render` owns terminal presentation: colour detection, layout, tables, trees, and plain-language wording for
shared result codes. It is the only package that imports the styling library. `internal/status`, `internal/doctor`, and
`internal/control` build text views with it. Command wrappers own JSON process envelopes, thresholds, and exit codes.
The CLI owns bounded bundle archives and offline replay. `internal/control` orchestrates shared guarded submission and
pure assessments. It owns bounded waits; collection remains read-only.

## Evidence and bundles

Preserve resource identity, collection time, producer evidence time when available, and collection outcomes. An archive
captures observations over an interval. It is not an atomic snapshot of Kubernetes.

Missing fields, forbidden access, failed collection, absent optional APIs, and unsupported interpretation remain distinct.
Current Kubernetes reads do not establish recent operator observations. Condition transition times are not reconciliation
heartbeats.

Use approved field projections before writing evidence to temporary storage or output. Exclude Secrets, raw ConfigMap
contents, literal environment values, container arguments, arbitrary annotations, and self-init payloads by default.
Bundles do not collect logs.

Live and offline diagnosis consume the same normalized observations. The pinned client release defines the typed model
and rules. Bundles record the CLI build, policy thresholds, and analysis reference time under one format marker.
Offline analysis retains the captured times and makes no claim about current cluster state.
Bundle readers enforce format, path, and size limits without extracting arbitrary files.

## Controls

Pause sets `spec.paused`; maintenance sets `spec.maintenance.enabled`. Pause acknowledgement does not prove that work
already started has stopped. Maintenance completion requires observed annotation propagation to the relevant workloads.
Restart sets `spec.runtime.restartAt`; observation requires the requested marker, converged revisions and replica counts,
and UID-confirmed Ready Pods under supported RollingUpdate topology. It does not establish serving health or Raft quorum.

Maintenance annotations require workload reconciliation. When both controls are needed, enable and verify maintenance
before pausing. Resume and disable maintenance are separate steps. Never resume implicitly, clear an independent recovery
hold, or change cluster state to collect diagnostics.

Controls pin the target context, API group, namespace, name, and UID. Apply field-scoped changes with optimistic
concurrency. Report API acceptance, configuration presence, and operator or workload verification separately. Waits have
bounded timeouts. A timeout after submission does not prove that the change failed and does not trigger rollback.

Compatibility is declared per operator release and capability. API discovery alone does not establish a qualified
contract. See [compatibility](compatibility.md) for validated versions and limits.

## Command results

Command wrappers own output formats, failure thresholds, and exit codes. See [output and exit codes](output.md).
