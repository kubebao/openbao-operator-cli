# Implementation roadmap

The first release covers single-cluster diagnostics, local support bundles, offline diagnosis, and guarded pause and
maintenance controls. The console remains optional and retains enrollment, fleet history, and persistence.

## Implemented foundation

The CLI uses the shared client for public API projection, typed evidence, bounded collection, UID association, and pure
diagnostic rules. The CLI owns kubeconfig loading, command policy, output, exit codes, bundle
archives, and offline replay. See [shared client integration](client-migration.md) for qualification evidence.

Status, diagnostic policy selection, local support bundles without logs, offline replay, guarded pause/resume, and
maintenance controls are implemented. API adapters cover native and legacy API groups. Backup age
requires a declared threshold. The pinned client release defines the evidence model and diagnostic behavior. Support
bundles have one format marker and no separate evidence or rule-set versions.

## Next capabilities

1. Extend evidence and qualification for concrete diagnostic cases. Prioritize nonempty restore association and the
   backup, upgrade, and integration-condition rules. Add resource types only when a finding needs them. Keep unknown
   evidence distinct from failed collection and inapplicable APIs.
2. Add optional logs with per-container time, byte, and collection-duration limits. Distinguish current and previous
   logs. Record missing logs, truncation, and redactions. Logs require a separate disclosure policy.
3. Qualify each supported capability against actual operator admission and reconciliation in isolated clusters. Keep
   CLI command qualification distinct from library contracts, console qualification, and production support claims.

Keep console collection credentials and permissions read-only. Broader lifecycle execution requires a concrete workflow
and a public API that correlates requests with results; it remains outside the first release. General OpenBao
administration continues through `bao`.

GitHub Actions remain disabled during initial CLI development. Local checks are required and do not represent hosted CI.
