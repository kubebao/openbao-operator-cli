# Security

This project is in initial development. It has no released or qualified operator compatibility range.

Report suspected vulnerabilities privately to a repository maintainer. Use GitHub private vulnerability reporting when
the repository offers that option. Do not put credentials, kubeconfigs, unreviewed support bundles, or sensitive logs in
issues or pull requests.

## Implementation boundaries

The current executable only prints help and build information. It does not access Kubernetes or OpenBao.

Planned collectors use approved field projections and exclude Secret reads by default. Logs require a separate option
because they can contain credentials or application data. Redaction cannot guarantee that a bundle contains no sensitive
information; review captured evidence before sharing it.

Planned controls use the caller's Kubernetes credentials and ordinary admission checks. They must preserve resource
identity, unrelated configuration, and independent recovery restrictions. See [architecture](docs/architecture.md).

## Development checks

Go security linting and `govulncheck` run in the normal CI workflows. CodeQL and dependency review are included but require
separate enablement for this private repository. Their configuration is described in [development](docs/development.md).
