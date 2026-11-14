# kubebao

Companion CLI for the [OpenBao Operator](https://github.com/kubebao/openbao-operator).

The CLI reports operator-published cluster status, diagnoses captured observations, creates local support bundles,
and submits guarded pause, resume, maintenance, and rolling-restart controls.

The CLI is unreleased. Build it from source for now. GitHub downloads, Homebrew, and Krew are prepared distribution
channels; their public installation commands become available after publication. See [installation](docs/installation.md).

## Build and run

Use Go 1.27.4 or a newer compatible toolchain. Go can download the required toolchain when `GOTOOLCHAIN=auto`.

```sh
make build
./bin/kubebao --help
./bin/kubebao version
./bin/kubebao version --output json
```

Help and version commands require no kubeconfig, Kubernetes cluster, console, or backend service.

Version JSON uses `schemaVersion: "v1"` and contains `version`, `commit`, `goVersion`, `os`, and `arch` string fields.
Successful commands return exit code `0`. Invalid input or an execution failure returns `1`. Command results go to
standard output; errors go to standard error.

## Cluster status

```sh
./bin/kubebao status prod -n security
./bin/kubebao status prod -n security --verbose
./bin/kubebao status prod -n security --output json
./bin/kubebao status prod -n security --watch
```

Status uses standard kubeconfig loading. Select a context with `--context` or a file with `--kubeconfig`. The command
reads the named `OpenBaoCluster` and API discovery endpoints. It reports operator observations without probing OpenBao,
reading Secrets, or changing cluster state. See [status](docs/status.md) for output, permissions, watch behavior, and
interpretation limits.

## Diagnosis

```sh
./bin/kubebao doctor prod -n security
./bin/kubebao doctor prod -n security --output json
./bin/kubebao doctor prod -n security --fail-on error
./bin/kubebao doctor prod -n security --max-backup-age 48h
```

`doctor` adds bounded, ownership-aware StatefulSet, Pod, and restore collection. It reports findings with stable rule
identifiers, resource and field references, suggested checks, and missing evidence. Rules consume captured observations
without making network requests. See [diagnosis](docs/doctor.md) for permissions, initial rule coverage, and exit codes.

## Support bundles and offline diagnosis

```sh
./bin/kubebao support-bundle prod -n security --output-file prod-support.tar.gz
./bin/kubebao doctor --bundle prod-support.tar.gz
```

Bundles contain approved evidence, collection results, findings, and a readable summary. Collection uses the same
permissions and bounds as `doctor`. Logs are not collected. Review names and approved metadata before sharing a bundle.
See [support bundles](docs/support-bundles.md) for the format, limits, partial results, and offline behavior.

## Cluster controls

```sh
./bin/kubebao pause prod -n security
./bin/kubebao resume prod -n security --wait
./bin/kubebao maintenance enable prod -n security --wait
./bin/kubebao maintenance disable prod -n security --dry-run
./bin/kubebao restart prod -n security --wait --wait-timeout 5m
```

Controls submit one field-scoped JSON Patch guarded by UID, resource version, and generation. API acceptance and later
operator observation are separate results. Maintenance never resumes reconciliation. See [controls](docs/controls.md)
for dry-run, observation, permissions, and unresolved submission outcomes.

## Planned scope

- Extend status with related-resource evidence and qualification against operator releases.
- Extend diagnostic evidence to tenants, Jobs, Events, and networking resources.
- Add bounded, optional log collection to support bundles.

The operator retains responsibility for reconciliation and lifecycle safety. General OpenBao administration continues
through `bao`. See [architecture and scope](docs/architecture.md) and the [roadmap](docs/roadmap.md).

## Development

```sh
make install
make check
make vulncheck
make check-licenses
```

`make check` runs the pinned Go linters, race tests, and binary build. See [development](docs/development.md) and
[contributing](CONTRIBUTING.md) for commands and conventions.

## Local distribution archives

```sh
make dist DIST_VERSION=0.0.0-dev
make verify-dist
```

This uses pinned GoReleaser and Syft binaries to build archives for Linux and macOS on amd64 and arm64. It also writes
SPDX SBOMs, a source archive, license notices, SHA-256 checksums, a source-building Homebrew formula, and a Krew manifest.
Apple Developer ID signing and notarization are optional and disabled by default. This command does not publish a release or
update package repositories. Verify the checksums from the archive directory:

```sh
cd dist
shasum -a 256 -c checksums.txt
```

See [release procedures](docs/releases.md) for publication gates, signing, provenance, and package promotion.

The project uses the [Apache License 2.0](LICENSE). Bundled dependency notices are in [THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES).
