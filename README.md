# kubebao

`kubebao` is the companion CLI for the [OpenBao Operator](https://github.com/kubebao/openbao-operator). It shows what
the operator reports about your OpenBao clusters, diagnoses common problems, packages evidence for support, and submits
guarded pause, maintenance, and restart requests.

It works through your kubeconfig and the Kubernetes API. It never reads Secrets, never contacts OpenBao directly, and
changes a cluster only when you run a control command.

> [!NOTE]
> `kubebao` is not released yet. Build it from source. Release archives, Homebrew, and Krew are prepared but not
> published; see [installation](docs/installation.md).

```text
$ kubebao doctor prod -n security
Diagnosis of prod  namespace security · context production

! 1 warning, 1 note
Rules: 12 evaluated · 3 not selected

! WARNING  Pod readiness: Pod prod-1  KBAO-POD-001
  The UID-associated Pod reports Ready=False for its current generation.
  Inspect the UID-confirmed workload and its reported readiness.
  → Inspect it:
      kubectl --context production -n security describe pod prod-1

• INFO  Operator conditions: EtcdEncryptionWarning  KBAO-CONDITION-001
  The operator cannot verify that the Kubernetes API server encrypts Secrets at rest in etcd.
  …

Resources
  OpenBaoCluster prod
  ╰── StatefulSet prod  2/3 ready
      ├── Pod prod-0  Running · ready
      ├── Pod prod-1  Running · not ready (ContainersNotReady)
      ╰── Pod prod-2  Running · ready
```

## Install

Build from source with Go 1.27.4 or newer. With `GOTOOLCHAIN=auto`, Go downloads the required toolchain.

```sh
make build
./bin/kubebao version
```

To use it as a kubectl plugin, put the binary on your `PATH` as `kubectl-kubebao`, then run `kubectl kubebao`. After
publication, Homebrew installs `kubebao`, the plugin, and shell completions; Krew installs the plugin. See
[installation](docs/installation.md) for every channel and for checksum verification.

## Quick start

`kubebao` uses standard kubeconfig loading. Select a file with `--kubeconfig`, a context with `--context`, and a
namespace with `-n`.

```sh
kubebao list -A                          # every OpenBaoCluster and what needs attention
kubebao status prod -n security          # what the operator reports for one cluster
kubebao doctor prod -n security          # check the cluster, its StatefulSets, Pods, and restores
kubebao support-bundle prod -n security --output-file prod-support.tar.gz
kubebao doctor --bundle prod-support.tar.gz   # diagnose the bundle offline
```

To change a cluster, submit a control and wait for the operator to act on it:

```sh
kubebao maintenance enable prod -n security --wait
kubebao restart prod -n security --dry-run
kubebao restart prod -n security --wait --wait-timeout 10m
```

## Commands

| Command | What it does | Changes the cluster |
| --- | --- | --- |
| [`list`](docs/status.md#list-clusters) | Lists clusters in a namespace, or all with `-A`, with version, readiness, controls, and attention | No |
| [`status`](docs/status.md) | Shows what the operator reports for one cluster; `--watch` refreshes it | No |
| [`doctor`](docs/doctor.md) | Diagnoses a cluster and the resources it owns, live or from a bundle | No |
| [`support-bundle`](docs/support-bundles.md) | Saves the doctor evidence and findings to a local archive | No |
| [`pause`, `resume`](docs/controls.md) | Pauses or resumes operator reconciliation | Yes |
| [`maintenance enable`, `disable`](docs/controls.md) | Sets maintenance mode | Yes |
| [`restart`](docs/controls.md#rolling-restart) | Requests an operator-managed rolling restart | Yes |
| `completion` | Prints a bash, zsh, fish, or PowerShell completion script | No |
| `version` | Prints build information without cluster access | No |

Run `kubebao COMMAND --help` for flags and examples.

## How kubebao stays safe

- **Read-only by default.** `list`, `status`, `doctor`, and `support-bundle` only read. They never pause a cluster or
  enable maintenance to collect evidence.
- **Reported, not probed.** Values come from the operator's status and from Kubernetes resources. kubebao does not probe
  OpenBao, so a report describes what the operator observed, not live serving health.
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

## Output and scripting

Every reporting command supports `--output text` (the default) and `--output json`. JSON is the stable, versioned
interface for scripts. Text output is for people and can change between releases.

Text output uses colour and symbols on a terminal: `✓` good, `!` warning, `✗` failure, `?` unknown, `•` note. Select
`--color auto`, `always`, or `never`; `auto` honours `NO_COLOR`. Pipes, files, and JSON never receive escape sequences.
Results go to standard output and errors to standard error.

| Exit code | Meaning |
| --- | --- |
| `0` | The command completed. A status report can still describe an unhealthy cluster. |
| `1` | Invalid input, configuration error, missing cluster, ambiguous API group, or a rejected control. |
| `2` | `doctor` found a finding at or above `--fail-on` (default `warning`). |
| `3` | Evidence is incomplete, the cluster changed during the command, or a control outcome is unknown or not yet observed. |

`--version` has no short form: `-v` means `--verbose` on `status` and `doctor`.

## Permissions

kubebao uses your own Kubernetes credentials and needs only these permissions in the selected namespace, plus access
to API discovery for `kubebao.org` or `openbao.org`:

| Command | Permissions |
| --- | --- |
| `status` | `get` on `openbaoclusters` |
| `list` | `list` on `openbaoclusters`; cluster-wide with `-A` |
| `doctor`, `support-bundle` | `get` on `openbaoclusters`; `list` on `statefulsets`, `pods`, and `openbaorestores` |
| `pause`, `resume`, `maintenance`, `restart` | `get` and `patch` on `openbaoclusters`; with `--wait`, maintenance and restart also `list` `statefulsets` and `pods` |

No command needs Secret access, Pod logs, `exec`, or port forwarding. If both `kubebao.org` and `openbao.org` serve
`OpenBaoCluster`, choose one with `--api-group`.

## Shell completion

```sh
source <(kubebao completion bash)                                  # bash
kubebao completion zsh > "${fpath[1]}/_kubebao"                    # zsh
kubebao completion fish > ~/.config/fish/completions/kubebao.fish  # fish
```

Completion suggests commands, flag values, kubeconfig contexts, and cluster names in the selected namespace. For
`kubectl kubebao`, install the `kubectl_complete-kubebao` helper; see [installation](docs/installation.md#shell-completion).

## Documentation

| Topic | Document |
| --- | --- |
| Install channels, kubectl plugin, completion | [Installation](docs/installation.md) |
| Status, list, watch mode, and interpretation limits | [Status](docs/status.md) |
| Diagnostic rules, coverage, and evidence gaps | [Diagnosis](docs/doctor.md) |
| Bundle format, limits, and offline diagnosis | [Support bundles](docs/support-bundles.md) |
| Pause, maintenance, restart, and observation | [Controls](docs/controls.md) |
| Scope and design | [Architecture](docs/architecture.md) |
| Shared client and operator qualification | [Client integration](docs/client-migration.md) |
| Planned work | [Roadmap](docs/roadmap.md) |
| Building, testing, and packaging | [Development](docs/development.md) and [releases](docs/releases.md) |

## Scope

The operator owns reconciliation and lifecycle safety. kubebao does not run backups, upgrades, restores, scaling, or
installation, and it does not repair clusters automatically. Use `bao` for OpenBao administration.

Planned, not yet available:

- Related-resource evidence in `status`, and qualification against operator releases.
- Diagnostic evidence for tenants, Jobs, Events, and networking resources.
- Bounded, optional log collection in support bundles.

## Development

```sh
make install
make check        # pinned linters, race tests, and the binary build
make vulncheck    # after dependency changes
make dist DIST_VERSION=0.0.0-dev && make verify-dist   # local release archives; never publishes
```

See [development](docs/development.md) and [contributing](CONTRIBUTING.md) for conventions, and
[releases](docs/releases.md) for packaging, signing, and publication.

## License

kubebao uses the [Apache License 2.0](LICENSE). Bundled dependency notices are in
[THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES).
