# Contributing

Use Go 1.27.3 or a newer compatible toolchain, Make, and Bash. Distribution packaging also uses `tar` and `shasum`.
Go downloads pinned development tools when their Make targets run.

```sh
make install
make check
```

Run `make lint-fix` to apply Go formatting and supported lint fixes. Run `make vulncheck` after dependency changes.
Regenerate dependency notices with `make licenses`, then verify them with `make check-licenses`.

Use conventional branch names, such as `feat/cluster-status`, and scoped Conventional Commits, such as
`feat(status): report observed generation`. Sign off commits with `git commit -s` under the
[Developer Certificate of Origin](https://developercertificate.org/).

Keep pull requests focused. Explain the resulting behavior, material compatibility or security implications, and the
checks you ran. Follow the boundaries in [AGENTS.md](AGENTS.md) and the [architecture](docs/architecture.md).

Report security vulnerabilities through a private channel as described in [SECURITY.md](SECURITY.md).
