# Engineering rules

Write clear technical English for developers and operators. Use short, direct sentences and exact API terminology.

## Boundaries

- This repository builds the `kubebao` CLI. The operator owns reconciliation and lifecycle safety.
- Use public operator APIs through version-specific adapters. Do not import operator or console `internal` packages.
- Keep collection, normalized evidence, diagnostic rules, bundle handling, controls, and rendering separate.
- Diagnostic rules consume evidence without network access. Live and offline analysis use the same rules.
- Project approved fields before writing evidence, temporary files, JSON, or debug output. Do not read Secrets in the
  default collector or log raw Kubernetes resources, credentials, or provider errors.
- Separate collection time, producer observation time, and condition transition time. Missing evidence remains unknown.
- Associate resources through UIDs, owner references, and documented metadata. Shared names or labels alone do not prove
  ownership.
- Keep support collection read-only. Never pause a cluster or enable maintenance to collect a bundle.
- Keep controls field-scoped and bound to the selected UID. Use optimistic concurrency and bounded waits. Distinguish API
  acceptance, configuration presence, and operator or workload verification.
- Pause and maintenance are independent. Never resume reconciliation implicitly to propagate a maintenance change.
- Treat support archives as untrusted input. Bound reads and reject unsafe paths and unsupported formats.
- Mark planned features as planned. Do not present scaffold or fixture checks as operator integration qualification.

## Code

- Use concrete types, bounded enums, and narrow interfaces owned by their consumer.
- Validate external input at integration boundaries. Keep core interpretation logic typed.
- Prefer focused packages. Add feature packages when their implementation needs them.
- Handle errors with context. Keep machine-readable results on standard output and errors on standard error.
- Keep lint and formatting settings in `.golangci.yml`. Add narrow, explained exclusions only when necessary.
- Pin tools and dependencies. Regenerate `THIRD_PARTY_LICENSES` with `make licenses` after linked dependency changes.
- Test externally observable command behavior, evidence interpretation, collection failures, archive handling, and control
  safety as those features are added.
- Run `make check` before handing work back. Run `make vulncheck` for dependency changes and `make dist` for packaging changes.

## Local work

- Use conventional branch names such as `feat/cluster-status` and scoped Conventional Commits.
- Sign off commits under the Developer Certificate of Origin.
- Preserve other repositories and existing local environments. Use isolated clusters for integration tests.
- Update documentation when command behavior or support boundaries change.
