# Source-first publication

The initial public repository provides source builds. Release archives, the Homebrew tap, and the Krew index entry
remain unpublished. Opening the source does not approve a release, package promotion, Apple signing, or a deployment.
Keep the unreleased notice and the documented operator qualification limits.

## Prepare while private

1. Keep `CLI_RELEASES_ENABLED=false` and `CLI_PACKAGES_ENABLED=false`. The manual publishing jobs require the
   corresponding opt-in and a public canonical repository. Tag pushes never publish. Ordinary Check runs can build
   and retain test archives without creating a release or updating a package channel.
2. Enable Actions with selected action names, SHA pinning, and a read-only default workflow token. Enable Dependabot
   alerts. Run Check on the current `main` commit. CodeQL and Dependency Review skip while private unless separately
   licensed; those skips do not establish public security qualification.
3. Review remote branches, tags, and advertised pull-request refs, and scan their Git history. Review documentation,
   examples, discussions, retained Actions logs, and artifact inventories. Store raw findings outside the repository.
4. Run `make check`, `make secret-scan`, `make vulncheck`, `make check-licenses`, and `make release-check`. CI also
   builds and verifies the four-platform snapshot archives. Keep the license, dependency notices, security policy,
   contribution guidance, and [qualification record](client-migration.md) current.

`make secret-scan` uses pinned Gitleaks, redacts findings, and scans all locally available refs. CI fetches full history.
Use a fresh remote mirror for the publication audit so advertised pull-request refs are included. No historical
findings are suppressed in `.gitleaksignore`. Review new findings rather than excluding whole test directories.

## Apply controls at the approved visibility change

Changing visibility is a separate action. These commands do not change visibility. Once the repository is public,
apply and verify them before accepting contributions:

```sh
gh api --method PUT repos/kubebao/openbao-operator-cli/branches/main/protection \
  --input .github/main-protection.json
gh api --method PATCH repos/kubebao/openbao-operator-cli --input .github/public-security.json
gh api --method PUT repos/kubebao/openbao-operator-cli/private-vulnerability-reporting
gh api --method PUT repos/kubebao/openbao-operator-cli/vulnerability-alerts
```

Branch protection requires current Go, vulnerability, license, distribution, secret-scan, CodeQL, Dependency Review,
and DCO checks. It requires resolved conversations and linear history, blocks force pushes and deletion, and applies
to administrators. This single-maintainer repository sets no minimum approval count. Confirm that each configured
check name and application ID matches an actual pull-request run.

Run Check and CodeQL on the same current `main` commit, then test Dependency Review with a pull request. Inspect
secret-scanning alerts and verify the private vulnerability-reporting form. Check anonymous source access and the
README's source-build commands without GitHub credentials. Keep both publication gates false and confirm that no
releases or tags were created. Record the source commit and successful workflow results.

## Qualify downloads later

The protected release environments, immutable-release setting, reviewed release notes, native-platform rehearsal,
public package repositories, and promotion credentials belong to a separate artifact-publication stage. They are not
required to open the source. Follow [release procedures](releases.md) when that stage is approved. Unsigned artifacts
remain the default; Apple signing is optional. Homebrew and upstream Krew acceptance remain separate checks.
