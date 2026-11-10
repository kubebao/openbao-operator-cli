# Security

This project is in initial development. It has no released or qualified operator compatibility range.

Report suspected vulnerabilities privately to a repository maintainer. Use GitHub private vulnerability reporting when
the repository offers that option. Do not put credentials, kubeconfigs, unreviewed support bundles, or sensitive logs in
issues or pull requests.

## Implementation boundaries

The status command reads the named `OpenBaoCluster` and API discovery endpoints through the caller's kubeconfig. It
does not read Secrets, query OpenBao, or mutate Kubernetes. Standard kubeconfig authentication can invoke credential
plugins configured by the caller.

Status projects approved fields and applies additional redaction to known credential assignments and bearer tokens in
diagnostic text. It excludes arbitrary annotations, configuration payloads, backup object paths, and break-glass nonces.
Free-form diagnostic text can still contain sensitive information; review output before sharing it. The CLI classifies
Kubernetes errors instead of returning API response bodies. Credential plugins can write their own diagnostics to
standard error as part of their normal behavior.

Planned collectors use approved field projections and exclude Secret reads by default. Logs require a separate option
because they can contain credentials or application data. Redaction cannot guarantee that a bundle contains no sensitive
information; review captured evidence before sharing it.

Planned controls use the caller's Kubernetes credentials and ordinary admission checks. They must preserve resource
identity, unrelated configuration, and independent recovery restrictions. See [architecture](docs/architecture.md).

## Development checks

Go security linting and `govulncheck` run in the normal CI workflows. CodeQL and dependency review are included but require
separate enablement for this private repository. Their configuration is described in [development](docs/development.md).
