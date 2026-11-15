# Contributing

Use Go 1.27.4 or a newer compatible toolchain, Make, and Bash. Distribution packaging also uses `tar` and `shasum`.
Go downloads pinned development tools when their Make targets run.

```sh
make install
make check
```

Run `make lint-fix` to apply Go formatting and supported lint fixes. Run `make vulncheck` after dependency changes.
Run `go mod tidy` after dependency changes; CI rejects drift in `go.mod` and `go.sum`.
Regenerate dependency notices with `make licenses`, then verify them with `make check-licenses`.

Use pull requests for changes to public `main`; required checks and review conversations must pass before merging.
Maintainers can work on `main` during private preparation. Use scoped Conventional
Commits, such as `feat(status): report observed generation`. Use a conventional branch name such as
`feat/cluster-status`. Sign off commits with `git commit -s` under the
[Developer Certificate of Origin](https://developercertificate.org/).

Run `make secret-scan` to scan reachable Git history with pinned Gitleaks. The scan redacts detected values.
Run `make check-workflows` after changing GitHub Actions.

Keep changes focused. Explain the resulting behavior, material compatibility or security implications, and the checks
you ran. Follow the boundaries in [AGENTS.md](AGENTS.md) and the [architecture](docs/architecture.md).

Report security vulnerabilities through a private channel as described in [SECURITY.md](SECURITY.md).
