<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/kubebao-logo-dark.webp">
  <img src="docs/images/kubebao-logo.webp" alt="KubeBao" width="160">
</picture>

# OpenBao Operator CLI

**A command-line tool to inspect, diagnose, and control clusters managed by the
[OpenBao Operator](https://github.com/kubebao/openbao-operator).**

[Quick start](#quick-start) • [Install](docs/installation.md) • [Documentation](#documentation) • [Contributing](CONTRIBUTING.md)

</div>

> [!NOTE]
> **Not released yet.** Build `kubebao` from source. Release archives, Homebrew, and Krew are prepared but not
> published. Edge and tagged release publishing remain disabled. See [installation](docs/installation.md).

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

The OpenBao Operator CLI, `kubebao`, shows what the OpenBao Operator reports about your OpenBao clusters, diagnoses common problems, packages
evidence for support, and submits guarded pause, maintenance, and restart requests.

It works through your kubeconfig and the Kubernetes API. It never reads Secrets, never contacts OpenBao directly, and
changes a cluster only when you run a control command. The operator owns reconciliation and lifecycle safety. See the
[safety model](docs/architecture.md#safety-model).

OpenBao Operator CLI is maintained by [KubeBao](https://kubebao.org), an independent open-source project. It is not an official
OpenBao project.

## Features

- **Status at a glance.** `list` shows every `OpenBaoCluster` in a namespace or across all namespaces, with what needs
  attention. `status` shows what the operator reports for one cluster and can refresh with `--watch`.
- **Diagnosis, live or offline.** `doctor` checks a cluster and the StatefulSets, Pods, and restores it owns. Each
  finding has a rule ID and a next step. The same rules run on a support bundle without cluster access.
- **Support bundles.** `support-bundle` saves the diagnostic evidence and findings to a local archive that you can
  review before you share it.
- **Guarded controls.** `pause`, `resume`, `maintenance`, and `restart` each change one field, fail if the cluster
  changed since it was read, and can wait until the operator acts.
- **Built for scripts.** Every reporting command has versioned JSON output and
  [documented exit codes](docs/output.md).

`kubebao` does not run backups, upgrades, restores, scaling, or installation, and it does not repair clusters
automatically. Use `bao` for OpenBao administration. Validated operator versions and their limits are in
[compatibility](docs/compatibility.md).

## Quick start

Build from source with Go 1.27.4 or later. With `GOTOOLCHAIN=auto`, Go downloads the required toolchain.

```sh
git clone https://github.com/kubebao/openbao-operator-cli.git
cd openbao-operator-cli
make build
./bin/kubebao version
```

With Nix and flakes enabled, build from the same checkout with `nix build .#kubebao`, or run
`nix run .#kubebao -- version`. See [Nix installation](docs/nix.md) for pinned inputs and package checks.

`kubebao` uses standard kubeconfig loading. Select a file with `--kubeconfig`, a context with `--context`, and a
namespace with `-n`. Replace `prod` and `security` with your cluster name and namespace.

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

To use `kubebao` as a kubectl plugin, put the binary on your `PATH` as `kubectl-kubebao`, then run `kubectl kubebao`.
The [installation guide](docs/installation.md) covers every channel, checksum verification, and shell completion.

## Requirements

| Component | Requirement |
| --- | --- |
| OpenBao Operator | Clusters managed by the [OpenBao Operator](https://github.com/kubebao/openbao-operator) |
| Kubernetes | 1.34 or later |
| Access | A kubeconfig with the [permissions](docs/installation.md#permissions) for the commands you run |
| Build | Go 1.27.4 or later, Make, and Bash |

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
| [`completion`](docs/installation.md#shell-completion) | Prints a bash, zsh, fish, or PowerShell completion script | No |
| `version` | Prints build information without cluster access | No |

Run `kubebao COMMAND --help` for flags and examples.

## Documentation

**Get started:** [install](docs/installation.md) · [observe a cluster](docs/status.md) ·
[diagnose a cluster](docs/doctor.md)

**Operate:** [collect a support bundle](docs/support-bundles.md) · [pause, maintenance, and restart](docs/controls.md)

**Reference:** [output and exit codes](docs/output.md) · [architecture and safety model](docs/architecture.md) ·
[compatibility](docs/compatibility.md) · [development](docs/development.md) · [releases](docs/releases.md)

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for the workflow and conventions, and
[development](docs/development.md) for checks and packaging. Report security issues privately as described in
[SECURITY.md](SECURITY.md).

## License

Licensed under the [Apache License 2.0](LICENSE). Third-party notices are in
[THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES).

The KubeBao name and logo identify the KubeBao project. The Apache License 2.0 does not grant permission to use them, as
stated in section 6 of the license.
