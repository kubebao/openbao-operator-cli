# Architecture and scope

## Current implementation

The repository contains a Go executable with Cobra command parsing, help, version output, build metadata, local archive
packaging, and development checks. It has no Kubernetes client or implemented cluster commands.

## Planned first release

`kubebao` connects to Kubernetes through the caller's kubeconfig. It requires neither the console nor a backend service.
The operator retains responsibility for reconciliation and lifecycle safety.

The proposed first release includes status and progress reporting, evidence-backed diagnosis, local support bundles,
offline diagnosis, pause and resume, and maintenance enable and disable controls. Backup execution, upgrades, restores,
restarts, scaling, installation, and automatic repair remain outside that release's scope. General OpenBao administration
continues through `bao`.

The implementation will separate:

- Kubernetes discovery, resource association, and collection.
- Version-specific adapters into normalized, sanitized observations.
- Diagnostic rules that consume observations without network access.
- Bundle reading and writing.
- Guarded control requests and their verification predicates.
- Human-readable and versioned JSON rendering.

These components do not yet exist. Add them with their first implemented capability. Share evidence semantics and
representative fixtures with the console; extract a public interpretation package only after its boundary is established.

## Evidence and bundles

Preserve resource identity, collection time, producer evidence time when available, and collection outcomes. An archive
captures observations over an interval. It is not an atomic snapshot of Kubernetes.

Missing fields, forbidden access, failed collection, absent optional APIs, and unsupported interpretation remain distinct.
Current Kubernetes reads do not establish recent operator observations. Condition transition times are not reconciliation
heartbeats.

Use approved field projections before writing evidence to temporary storage or output. Exclude Secrets, raw ConfigMap
contents, literal environment values, container arguments, arbitrary annotations, and self-init payloads by default.
Optional logs require time, byte, and duration limits. Record omissions, truncation, and redactions without promising
complete removal of sensitive information.

Live and offline diagnosis consume the same normalized observations. Record rule-set versions, policy thresholds, and
the analysis reference time. Offline analysis retains the captured times and makes no claim about current cluster state.
Bundle readers enforce format, path, and size limits without extracting arbitrary files.

## Controls

Pause sets `spec.paused`; maintenance sets `spec.maintenance.enabled`. Pause acknowledgement does not prove that work
already started has stopped. Maintenance completion requires observed annotation propagation to the relevant workloads.

Maintenance annotations require workload reconciliation. When both controls are needed, enable and verify maintenance
before pausing. Resume and disable maintenance are separate steps. Never resume implicitly, clear an independent recovery
hold, or change cluster state to collect diagnostics.

Controls pin the target context, API group, namespace, name, and UID. Apply field-scoped changes with optimistic
concurrency. Report API acceptance, configuration presence, and operator or workload verification separately. Waits have
bounded timeouts. A timeout after submission does not prove that the change failed and does not trigger rollback.

Compatibility is declared per operator release and capability. API discovery alone does not establish a qualified
contract. No operator release is qualified by this scaffold.

## Planned command results

The implemented help and version commands use `0` for completion and `1` for invalid input or execution failure.
Diagnostic and control commands will add:

- `2`: diagnosis finds an issue at or above the selected failure threshold.
- `3`: collection or verification is incomplete, including an unresolved outcome after submission.

Incomplete diagnosis takes precedence in the exit code while preserving confirmed findings in output. Status does not
fail only because the cluster is unhealthy. A complete bundle can contain serious findings and still return success.
