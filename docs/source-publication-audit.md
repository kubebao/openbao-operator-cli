# Source publication audit

Audit date: 2026-11-14. Source baseline: `ff6d852a9b0f158efeb9a1bceb39a6578d50a637`.
This record covers preparation for public source access. It does not authorize a visibility change or artifact release.

## History and public-facing material

A fresh remote mirror contained 26 reachable commits, two branch refs, and two advertised pull-request refs. Pinned
Gitleaks `v8.30.1` found no credentials. No historical findings are suppressed. Review covered source-build instructions,
examples, security and compatibility documentation, historical file paths, and the two available discussion/review
records. Targeted private-host, local-path, and customer-reference searches found no matches.

All nine retrievable log archives among ten recorded workflow runs were scanned; one unavailable archive could not be
inspected. The combined console/CLI text scan's 390 detections were individually classified as public Git identifiers
or expired registry download URLs. No finding was left unclassified. No remote history or workflow run was deleted
or rewritten. Raw reports and private logs remain outside this repository.

The two retained CLI artifacts were distribution snapshots. Review covered artifact inventories and workflow upload
paths. Snapshot verification checks the archive inventory, checksums, source identity, SBOMs, and package metadata.
This review does not qualify a published release, a native-platform rehearsal, or an operator integration.

## Preparation controls

- Actions is enabled with SHA pinning and the organization's existing selected-actions policy. Its default token is
  read-only and cannot approve pull requests. Dependabot alerts are enabled.
- `CLI_RELEASES_ENABLED=false` and `CLI_PACKAGES_ENABLED=false`. Ordinary checks and tag pushes do not publish.
- Check supports manual runs and includes workflow validation and a redacted full-history secret scan. CodeQL also
  supports manual qualification after visibility change.
- Public branch-protection and security settings are prepared as `.github/main-protection.json` and
  `.github/public-security.json`. Contribution instructions require pull requests once public. No release, tag,
  package repository, or deployment was created by preparation.

## Validation and remaining boundary

Local `make check`, `make secret-scan`, `make vulncheck`, `make check-licenses`, and `make release-check` pass. The
release-policy suite contains 14 tests. Current hosted Check results remain required for the preparation changes.

Apply and verify public controls, run actual CodeQL and Dependency Review, inspect secret-scanning results, and verify
anonymous source access at the separately approved visibility change. Follow [source publication](source-publication.md).
Keep artifact publication and package promotion held. Native-platform release rehearsals and package-channel
qualification remain separate work. Refresh this audit after new source, history, or public-facing material changes.
