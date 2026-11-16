{
  lib,
  buildGoModule,
  callPackage,
  installShellFiles,
  version,
  commit,
}:

let
  go = callPackage ./go.nix { };
in
(buildGoModule.override { inherit go; }) {
  pname = "kubebao";
  inherit version;

  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../cmd
      ../internal
      ../packaging/kubectl
      ../LICENSE
      ../THIRD_PARTY_LICENSES
    ];
  };
  vendorHash = "sha256-MPv7Jnz2wBli+XHvUVk1aOkt1PC2we5TuIg6oOsGZTM=";
  overrideModAttrs = _: { name = "kubebao-go-modules"; };
  subPackages = [ "cmd/kubebao" ];
  env = {
    CGO_ENABLED = "0";
    GOWORK = "off";
  };
  nativeBuildInputs = [ installShellFiles ];
  ldflags = [
    "-s"
    "-w"
    "-X github.com/kubebao/openbao-operator-cli/internal/buildinfo.Version=v${version}"
    "-X github.com/kubebao/openbao-operator-cli/internal/buildinfo.Commit=${commit}"
  ];
  checkPhase = ''
    runHook preCheck
    go test ./...
    runHook postCheck
  '';
  postInstall = ''
    ln -s "$out/bin/kubebao" "$out/bin/kubectl-kubebao"
    install -m755 packaging/kubectl/kubectl_complete-kubebao "$out/bin/"
    installShellCompletion --cmd kubebao \
      --bash <("$out/bin/kubebao" completion bash) \
      --zsh <("$out/bin/kubebao" completion zsh) \
      --fish <("$out/bin/kubebao" completion fish)
    install -Dm644 LICENSE "$out/share/licenses/kubebao/LICENSE"
    install -m644 THIRD_PARTY_LICENSES "$out/share/licenses/kubebao/THIRD_PARTY_LICENSES"
  '';
  meta = {
    description = "Diagnose and control clusters managed by the OpenBao Operator";
    homepage = "https://github.com/kubebao/openbao-operator-cli";
    license = lib.licenses.asl20;
    mainProgram = "kubebao";
    platforms = [
      "x86_64-linux"
      "aarch64-linux"
      "aarch64-darwin"
    ];
  };
}
