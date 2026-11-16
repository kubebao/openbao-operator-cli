# Install with Nix

The repository flake builds `kubebao` from source. It provides packages and apps for Linux on amd64 and arm64, and macOS
on arm64. Current nixpkgs does not support Intel macOS. Use a Go source build there, or the archive and Homebrew paths
after their publication.
`flake.lock` pins nixpkgs, and `vendorHash` pins the Go dependency closure. No GitHub release or package publication is
needed to build the source package.

`nix/go.nix` pins the official Go 1.27.4 source and checksum to meet the module's compiler requirement. A cold build
also builds that compiler. The resulting store paths can be reused by later local builds.

Enable the `nix-command` and `flakes` experimental features in your Nix configuration. From a checkout, run:

```sh
nix run .#kubebao -- version
nix build .#kubebao
./result/bin/kubebao --help
```

The default package and app are also available as `nix build .` and `nix run . -- version`. For a remote source, use:

```sh
nix run github:kubebao/openbao-operator-cli#kubebao -- version
```

This tracks the default branch. To keep the selected source fixed, replace `<revision>` with a full Git commit or a
released version tag:

```sh
nix run github:kubebao/openbao-operator-cli/<revision>#kubebao -- version
```

Before the first release, use a commit rather than an unreleased version tag. Source builds report the manifest version
with `+nix.<revision>` build metadata and the full source commit. The manifest defaults to `0.0.0` when absent. Dirty
checkouts remain identified as dirty; this metadata does not claim an official release build.

## Use the package

The output contains:

- `bin/kubebao`, the CLI;
- `bin/kubectl-kubebao`, the kubectl plugin symlink;
- `bin/kubectl_complete-kubebao`, the plugin completion helper;
- Bash, Zsh, and Fish completion files under `share`;
- the license and third-party notices under `share/licenses/kubebao`.

Add the package to your NixOS, nix-darwin, or Home Manager package list to expose these commands on `PATH`. Pin this
repository as a flake input. Reference `inputs.openbao-operator-cli.packages.<system>.kubebao`, replacing `<system>`
with your Nix system such as `aarch64-darwin` or `x86_64-linux`.

The package uses your kubeconfig when you run a cluster command. Building it and running its checks do not access a
cluster. See [installation permissions](installation.md#permissions) before running operator commands.

## Verify and update

```sh
nix flake check --all-systems --no-build
nix fmt -- --check flake.nix nix/*.nix
nix flake check --print-build-logs
```

The first command evaluates all three systems. The final command builds and tests the host package, version output,
kubectl entry point, plugin completion, and installed shell completion. The package build also runs the Go tests with
vendored dependencies. CI runs these checks natively on all three systems when Nix packaging, dependency inputs, or the
plugin completion helper changes. Other source changes retain the normal Go and archive checks; the Nix workflow also
supports manual runs.

Update nixpkgs with `nix flake update nixpkgs` and review the lockfile. After changing `go.mod` or `go.sum`, replace
`vendorHash` in `nix/package.nix` with `lib.fakeHash`, run `nix build .#kubebao`, and copy the reported dependency hash
into `vendorHash`. Run the checks again with the recorded hash. Keep these changes in the same PR as the dependency
update.

When changing the Go version requirement, update the compiler version and official source checksum in `nix/go.nix`.

These checks qualify source packaging and command startup. Operator compatibility and production limits remain in
[compatibility](compatibility.md).
