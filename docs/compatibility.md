# Compatibility

## Kubernetes

`kubebao` supports Kubernetes 1.34 and later. Pod readiness evaluation needs the kubelet to report `observedGeneration`
on Pod conditions. If a Pod condition omits it, Pod readiness coverage remains incomplete; the CLI never infers the
generation from omission.

## Shared client

The CLI pins the public client candidate `v0.2.1-0.20261114115514-8981014162d0`, commit
`8981014162d00a902584dff0e5a2b9d5fd519e5d`, in `go.mod`. The pinned client defines the typed
evidence model, API projections, guarded control plans, and diagnostic semantics. Evidence and diagnostic policies have
no separate schema or rule-set versions. The CLI keeps command JSON at `schemaVersion: "v1"` through presentation
mappings. Support bundles use one format marker.

Release builds use that exact dependency with `GOWORK=off` and no local replacement. The client's
[compatibility document](https://github.com/kubebao/openbao-operator-client/blob/8981014162d00a902584dff0e5a2b9d5fd519e5d/docs/compatibility.md)
describes its adapter sources and validation levels.

## API groups

Separate adapters read `kubebao.org/v1alpha1` and `openbao.org/v1alpha1`. Automatic discovery requires exactly one
group that serves `OpenBaoCluster`. `--api-group` reads only the selected group. After selection, the group and UID stay
pinned. Migration annotations do not establish identity across groups.

## Validation levels

### Distribution

The **Distribution rehearsal** workflow exercises Linux and macOS archives natively on amd64 and arm64. It verifies
the source commit, checksums, four binary SBOMs, Krew installation from the local archive, and a source rebuild. The
generated Homebrew formula is built and tested on macOS in a temporary prefix. This does not qualify Homebrew bottles,
Linux Homebrew installation, Apple signing, notarization, or managed-device download policies.

Packaging checks do not connect to an operator or extend the runtime baselines below.

Edge publication requires successful main Check and CodeQL jobs, authenticated CI provenance, and the same native
four-platform checks on the exact candidate payload. Tagged publication builds the selected version independently and
verifies it before finalizing an immutable release. An edge or stable channel label does not extend operator, Apple,
production, or HA qualification.

### Operator capabilities

| Level | Coverage | Limits |
| --- | --- | --- |
| Command tests | Synthetic text and JSON output, API selection, request paths and selectors, permissions, pagination and candidate limits, UID deletion and replacement, cancellation, timeouts, redirects, output failures, and sensitive-data exclusion | No API-server defaulting, admission, RBAC, or reconciliation |
| Observation and controls | Legacy operator `0.6.1` and native candidate `22894cdc0a4aa5c442a07f2bf9902fe28ce02ea1` on Kubernetes 1.36.1, Linux/arm64, OpenBao 2.7.2 | See [observation and controls](#observation-and-controls) |
| Nonempty restore observation | Native build `587dd16386bc5a234520e22eec1bbedb904fa6dd` and legacy operator `0.6.1` | See [restore observation](#restore-observation) |
| Rolling restart | Native operator `0.7.0-rc.1`, source `7b9bbf8b1a2aa3a62e2235607073df3e912643b5` | See [rolling restart](#rolling-restart) |

API discovery, compilation, and synthetic fixtures do not qualify an operator build. Each release records the exact CLI,
client, operator image and CRD, and Kubernetes identities it was qualified against in its release notes.

Synthetic restore-test command checks cover failed, blocked, active, passed, unknown, stale, and incomplete results.
They verify warning findings, incomplete-evidence precedence, sensitive-message exclusion, and identical live/offline
findings, coverage, and captured policy. These checks do not qualify restore-test execution against an operator.

### Observation and controls

The fixtures use disposable clusters with self-initialization, the Development profile, and enabled admission policies.
They cover voter-only clusters, positive and zero read pools, and an initialized revisioned blue/green pool in `Idle`.
API acceptance, operator acknowledgement, and workload annotations are separate results.

The Development fixtures report `UserAccessBootstrap=Unknown`. `doctor` reports incomplete condition coverage and exits
`3`. Bundle creation completes, and offline diagnosis reproduces the result.

### Restore observation

The native build runs in a migrated demo on Kubernetes `1.36.1+k3s1`, Linux/amd64. The legacy operator runs on
Kubernetes `1.36.1`, Linux/arm64. Both use OpenBao `2.7.2`. The legacy fixture harness verifies selected restored KV
data; the CLI reads only Kubernetes observations.

- **Native:** an unheld historical `Unknown` request and a fresh disposable target during cleanup retention. The CLI
  reports an informational finding and preserves the reported `Unknown` phase and stage. Older requests without UID
  bindings keep restore coverage insufficient.
- **Legacy:** a request that reports `Unknown` with a matching hold produces an error finding. Missing backup-counter
  evidence keeps the overall diagnosis incomplete.

Both baselines cover support-bundle collection and deterministic offline replay. A bundle can complete while diagnosis
exits `3`. To repeat the check, see the [read-only restore test](doctor.md#read-only-restore-qualification).

### Rolling restart

The fixtures run in disposable Kind clusters: one voter, three voters with one steady read replica, and one voter with a
zero-replica read pool. For each fixture, the test runs a server dry-run without persisted changes, submits one guarded
restart, waits for marker, revision, and readiness convergence, and checks that every original UID-confirmed Pod was
replaced. To repeat the check, see the [runtime test procedure](development.md#rolling-restart-runtime-qualification).

### Nix source packaging

The Nix workflow evaluates and builds the source flake natively on Linux/amd64, Linux/arm64, and macOS/arm64. It runs Go
tests and verifies source identity, CLI startup, the kubectl plugin entry point, plugin completion, and installed shell
completion. The lockfile, compiler source checksum, and Go vendor hash pin the build inputs. This does not extend
operator, Apple signing, or production qualification. Intel macOS is outside the current nixpkgs platform set; use the
archive or Homebrew installation path there.

## Unsupported

The validation above does not cover:

- other operator builds or a released native operator;
- HA or production operation, serving health, or end-to-end application availability;
- completed upgrades, active blue/green transitions, or backup execution;
- production recovery, every restore failure mode, or the correctness of restored application data;
- rolling restart on a legacy operator or a Blue/Green topology; or
- control behavior on Kubernetes 1.34.
