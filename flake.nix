{
  description = "Rho - AI coding agent powered by flux";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    # GrayCodeAI sibling repos — the public Go proxy has stale v0.1.0 tags
    # (post-history-rewrite), so resolve them locally in the Nix build.
    flux   = { url = "github:GrayCodeAI/flux";   flake = false; };
    merlin = { url = "github:GrayCodeAI/merlin"; flake = false; };
    kestrel   = { url = "github:GrayCodeAI/kestrel";   flake = false; };
    shrike     = { url = "github:GrayCodeAI/shrike";     flake = false; };
    swift   = { url = "github:GrayCodeAI/swift";   flake = false; };
    harrier    = { url = "github:GrayCodeAI/harrier";    flake = false; };
  };

  outputs = { self, nixpkgs, flake-utils, flux, merlin, kestrel, shrike, swift, harrier }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        inherit (pkgs) lib;

        siblings = {
          "github.com/GrayCodeAI/flux"   = flux;
          "github.com/GrayCodeAI/merlin" = merlin;
          "github.com/GrayCodeAI/kestrel"   = kestrel;
          "github.com/GrayCodeAI/shrike"     = shrike;
          "github.com/GrayCodeAI/swift"   = swift;
          "github.com/GrayCodeAI/harrier"    = harrier;
        };

        # GrayCode modules currently follow github.com/<org>/<name>, so the
        # last path segment matches the sibling checkout directory name.
        dirOf = mod: lib.last (lib.splitString "/" mod);
        goVer = lib.removePrefix "go" "${pkgs.go_1_26.version}";

        # Copy sibling sources into a private Nix build staging directory,
        # patch the go.mod go directive
        # to match the nixpkgs Go version, and add replace directives so the
        # build resolves siblings locally instead of hitting the stale proxy.
        setupReplace = ''
          sed -i 's/^go [0-9.]\+/go ${goVer}/' go.mod
          rm -f go.work go.work.sum
          mkdir -p deps
          ${lib.concatStringsSep "\n" (lib.mapAttrsToList (mod: src:
            "cp -r ${src} deps/${dirOf mod}"
          ) siblings)}
          ${lib.concatStringsSep "\n" (lib.mapAttrsToList (mod: _:
            "tmp_go_mod=$(mktemp) && awk '$0 != \"replace ${mod} => ./deps/${dirOf mod}\"' go.mod > \"$tmp_go_mod\" && mv \"$tmp_go_mod\" go.mod"
          ) siblings)}
          ${lib.concatStringsSep "\n" (lib.mapAttrsToList (mod: _:
            "echo \"replace ${mod} => ./deps/${dirOf mod}\" >> go.mod"
          ) siblings)}
        '';

        rho = pkgs.buildGoModule rec {
          pname = "rho";
          version = "0.0.1";

          src = ./.;

          # The public Go proxy has stale v0.1.0 tags for GrayCodeAI sibling
          # modules (post-history-rewrite). We resolve sibling sources locally
          # via replace directives in go.mod (added in preBuild below). Other
          # deps (charmbracelet, cobra, …) are fetched from the proxy. The
          # vendor FOD is skipped (null) because the proxy version mismatch
          # prevents `go mod vendor` from passing checksum verification.
          # TODO(supply-chain): vendorHash = null + GONOSUMCHECK disables
          # dependency integrity/reproducibility for this build. Restore a
          # fixed-output vendorHash ("sha256-…", computed via `nix build`
          # after setupReplace) so external deps are verified, and drop
          # GONOSUMCHECK for non-GrayCodeAI modules.
          vendorHash = null;

          env = {
            GOPRIVATE = "github.com/GrayCodeAI/*";
            GONOSUMDB  = "github.com/GrayCodeAI/*";
            GONOSUMCHECK = "1";
            GOFLAGS = "-mod=mod";
          };

          # Add replace directives to go.mod so the build and vendor FOD
          # resolve sibling modules locally instead of from the stale proxy.
          preBuild = setupReplace;
          overrideModAttrs = old: {
            preBuild = setupReplace;
          };

          ldflags = [
            "-s"
            "-w"
            "-X main.Version=${version}"
          ];

          nativeBuildInputs = [ pkgs.git ];

          meta = with lib; {
            description = "AI coding agent that reads, writes, and runs code in your terminal";
            homepage = "https://github.com/GrayCodeAI/rho";
            license = licenses.mit;
            maintainers = [ ];
          };
        };
      in
      {
        packages = {
          default = rho;
          inherit rho;
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go_1_26
            gopls
            gotools
            go-tools
            golangci-lint
            git
          ];

          shellHook = ''
            echo "Rho development shell"
            echo "Go version: $(go version)"
          '';
        };

        apps.default = {
          type = "app";
          program = "${rho}/bin/rho";
        };
      });
}
