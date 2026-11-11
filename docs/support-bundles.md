# Collect and diagnose a support bundle

`support-bundle` writes approved Kubernetes observations and a diagnostic report to a local archive. It uses the same
read-only collector and permissions as [doctor](doctor.md). It does not pause the cluster, enable maintenance, upload
data, read Secrets, or contact OpenBao or cloud APIs.

```sh
kubebao support-bundle prod -n security --output-file prod-support.tar.gz
kubebao support-bundle prod -n security --max-backup-age 48h --output-file prod-support.tar.gz --overwrite
kubebao support-bundle prod -n security --output-file prod-support.tar.gz --output json
```

The output directory must exist. The command refuses to replace an existing output file unless `--overwrite` is set.
It finalizes a mode `0600` temporary archive in that directory, then publishes it atomically. The temporary file is
removed after success or failure. No unprojected evidence enters temporary files.

Collection has the same candidate, page, byte, and duration bounds as `doctor`. An interrupt stops collection. The CLI
then allows up to ten seconds to finalize already captured evidence. A forced process termination can prevent cleanup
or publication.

## Contents and disclosure scope

Format `v1` contains four regular files:

```text
manifest.json
summary.txt
findings.json
evidence/capture.json
```

- `manifest.json` records the format and evidence schema versions, CLI build, selected target, collection interval,
  collection result and steps, requested timeout, log option, and diagnostic policy. The policy contains the selected
  rule IDs, rule-set version, projection contracts, thresholds, and analysis reference time.
- `summary.txt` contains captured times, collection results, findings, references, and coverage limits.
- `findings.json` contains the mapped findings, coverage, policy, and diagnostic completeness.
- `evidence/capture.json` contains the shared normalized observations. It is `null` when discovery or the initial
  cluster read prevents evidence capture. Named collection failures remain in the manifest.

The current collector covers the target cluster and bounded StatefulSet, Pod, and snapshot-restore candidates. It
retains admitted candidates, including foreign and unconfirmed candidates, so collection counts and offline association
checks remain reproducible. StatefulSet and Pod reads use the cluster selector; restore candidates come from a bounded
namespace LIST. Labels select candidates and do not prove ownership. The bundle can therefore include names, UIDs, and
approved operational fields for candidates that do not belong to the selected cluster. It contains no raw namespace dump.

Projection excludes raw ConfigMap contents, container arguments, literal environment values, arbitrary annotations,
authentication references, self-init payloads, and provider messages. Logs are not implemented, and no `logs/` directory
is present. Jobs, PVCs, Services, Events, tenant resources, and operator Deployments are not collected by this version.

Review a bundle before sharing it. Resource names, approved metadata, and operational observations can contain sensitive
information. Projection and known-pattern redaction do not guarantee that every sensitive value is removed.

## Results

Collection completeness and diagnostic completeness are separate:

- `0`: collection completes and the archive is published. Findings or incomplete rule coverage do not change this code.
- `1`: invalid input, configuration failure, invalid normalized evidence, archive failure, or output failure.
- `3`: collection is partial and a usable archive is published. The command prints its path.

JSON uses command schema `v1` with `formatVersion`, `outputFile`, `resultCode`, `collectionResult`,
`diagnosticComplete`, and `exitCode`. `resultCode` is `completed` or `partial`. A report-output failure can return `1`
after the archive is published; it does not remove that archive.

The bundle captures observations over an interval. It is not an atomic Kubernetes snapshot. Collection time, producer
observation time, and condition transition time retain their separate meanings. Failed or skipped reads do not prove
health. Skipped reads retain an unknown collection time.

## Offline diagnosis

```sh
kubebao doctor --bundle prod-support.tar.gz
kubebao doctor --bundle prod-support.tar.gz --verbose
kubebao doctor --bundle prod-support.tar.gz --output json --fail-on error
```

Offline diagnosis loads no kubeconfig and makes no network requests. It uses the captured evidence, policy, and analysis
reference time. It recomputes findings and coverage; it does not trust `summary.txt` or `findings.json` as evidence.
Equivalent live and offline inputs produce the same diagnostic findings, coverage, and rule version.

The report describes the captured cluster state. It does not establish the current state. Text identifies offline
analysis and captured times. JSON uses `source: "bundle"`. Kubernetes flags, a cluster name, and `--max-backup-age` cannot
be combined with `--bundle`. Output and failure-threshold options remain available. Doctor retains its usual exit codes,
including `2` for findings and `3` for incomplete collection or evaluation.

The reader accepts only supported bundle, evidence, and rule-set versions. It does not migrate old captures or change
their policy automatically. A future command for analysis with newer rules requires a separate contract.

## Archive limits

The reader opens a regular file, reads it without extracting entries, and enforces these limits:

| Limit | Value |
| --- | --- |
| Compressed archive | 64 MiB |
| Expanded archive | 96 MiB |
| `manifest.json` | 256 KiB |
| `summary.txt` | 2 MiB |
| `findings.json` | 8 MiB |
| `evidence/capture.json` | 64 MiB |
| JSON nesting | 64 levels |

The reader rejects unexpected paths, duplicate entries, links, non-regular entries, PAX extensions, missing files,
invalid gzip checksums, concatenated gzip streams, trailing nonzero tar data, invalid UTF-8, duplicate JSON keys, unknown
JSON fields in the manifest or capture, mismatched target identity, and unsupported versions. These checks validate
format and internal consistency. They do not authenticate the bundle's source.

Command and archive tests use synthetic Kubernetes responses. They cover replay, privacy exclusions, partial and
interrupted collection, file publication, output failures, and hostile inputs. They do not qualify additional operator
versions, resource types, lifecycle actions, or nonempty restore correlation against a live operator.
