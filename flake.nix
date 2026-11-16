{
  description = "OpenBao Operator CLI source package";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forSystems = nixpkgs.lib.genAttrs systems;
      commit = self.rev or (self.dirtyRev or "dirty");
      revision = self.shortRev or (self.dirtyShortRev or "dirty");
      manifest = ./.release-please-manifest.json;
      baseVersion =
        if builtins.pathExists manifest then
          (builtins.fromJSON (builtins.readFile manifest))."."
        else
          "0.0.0";
      version = "${baseVersion}+nix.${revision}";
    in
    {
      packages = forSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          kubebao = pkgs.callPackage ./nix/package.nix { inherit commit version; };
        in
        {
          inherit kubebao;
          default = kubebao;
        }
      );

      apps = forSystems (system: {
        default = self.apps.${system}.kubebao;
        kubebao = {
          type = "app";
          program = nixpkgs.lib.getExe self.packages.${system}.kubebao;
          meta.description = "Inspect and diagnose OpenBao Operator clusters";
        };
      });

      checks = forSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          goos = if nixpkgs.lib.hasSuffix "darwin" system then "darwin" else "linux";
          goarch = if nixpkgs.lib.hasPrefix "aarch64" system then "arm64" else "amd64";
        in
        {
          package = self.packages.${system}.kubebao;
          commands =
            pkgs.runCommand "kubebao-command-checks"
              {
                nativeBuildInputs = [
                  self.packages.${system}.kubebao
                  pkgs.jq
                ];
              }
              ''
                export KUBECONFIG="$TMPDIR/absent-kubeconfig"
                kubebao version --output json > version.json
                jq -e --arg commit '${commit}' --arg version 'v${version}' \
                  '.commit == $commit and .version == $version and .os == "${goos}" and .arch == "${goarch}"' version.json
                kubectl-kubebao doctor --help > help.txt
                test -s help.txt
                kubectl_complete-kubebao maintenance "" > completion.txt
                grep -q '^enable' completion.txt
                grep -q '^disable' completion.txt
                test -s "${self.packages.${system}.kubebao}/share/bash-completion/completions/kubebao.bash"
                mkdir "$out"
                cp version.json help.txt completion.txt "$out/"
              '';
        }
      );

      formatter = forSystems (system: nixpkgs.legacyPackages.${system}.nixfmt);
    };
}
