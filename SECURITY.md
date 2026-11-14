# Security

This project is in initial development. It has no released or qualified operator compatibility range.

Report suspected vulnerabilities privately to a repository maintainer. Use GitHub private vulnerability reporting when
the repository offers that option at <https://github.com/kubebao/openbao-operator-cli/security/advisories/new>.
If private reporting is unavailable, open a minimal issue requesting a private contact channel without exploit details.
Do not put credentials, kubeconfigs, unreviewed support bundles, or sensitive logs in
issues or pull requests.

## Implementation boundaries

The status command reads the named `OpenBaoCluster` and API discovery endpoints through the caller's kubeconfig. It
does not read Secrets, query OpenBao, or mutate Kubernetes. Standard kubeconfig authentication can invoke credential
plugins configured by the caller.

The doctor command also lists StatefulSet and Pod candidates using the documented cluster label, then verifies owner
references and UIDs before including them. It inspects a bounded namespace list of restore requests because the API has
no declared target-UID selector. Only requests correlated to the selected cluster UID or a documented cluster-side
request UID are included. Unverified candidates produce named omissions and incomplete collection.

Status projects approved fields and applies additional redaction to known credential assignments and bearer tokens in
diagnostic text. It excludes arbitrary annotations, configuration payloads, backup object paths, and break-glass nonces.
Free-form diagnostic text can still contain sensitive information; review output before sharing it. The CLI classifies
Kubernetes errors instead of returning API response bodies. Credential plugins can write their own diagnostics to
standard error as part of their normal behavior.

Support bundles use approved field projections and do not collect Secrets or logs. Live and offline analysis use the
same diagnostic rules. Resource names and approved observations can still contain sensitive information; review a
bundle before sharing it. The archive reader bounds input and rejects unsupported formats and unsafe entries.

Pause, maintenance, and restart controls use the caller's Kubernetes credentials and admission checks. Each request
patches a specific field with UID and resource-version preconditions. Unknown submission outcomes are not retried.
API acceptance, operator acknowledgement, and workload convergence are separate results. Support collection never
submits controls. See [architecture](docs/architecture.md) and [controls](docs/controls.md).

## Development checks

CI runs Go security linting, race tests, `govulncheck`, dependency-notice checks, workflow validation, and a redacted
Git-history secret scan. CodeQL and dependency review run when the repository is public; a private-repository skip is
not a passed scan. See [development](docs/development.md) and [source publication](docs/source-publication.md).
