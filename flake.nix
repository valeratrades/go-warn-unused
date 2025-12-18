{
  description = "Go compiler with -nounusederrors flag to treat unused errors as warnings";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };

        go-warn-unused = pkgs.go.overrideAttrs (oldAttrs: {
          pname = "go-warn-unused";

          patches = (oldAttrs.patches or []) ++ [
            ./patches/nounusederrors.patch
          ];

          meta = oldAttrs.meta // {
            description = "Go compiler with -nounusederrors flag";
          };
        });

      in
      {
        packages = {
          default = go-warn-unused;
          go = go-warn-unused;
        };

        overlays.default = final: prev: {
          go = go-warn-unused;
        };

        devShells.default = pkgs.mkShell {
          packages = [ go-warn-unused ];
          shellHook = ''
            echo "go-warn-unused ready. Use: go build -gcflags='-nounusederrors' ..."
          '';
        };
      }
    ) // {
      overlays.default = final: prev:
        let
          system = prev.system;
          pkgs = import nixpkgs { inherit system; };
          go-warn-unused = pkgs.go.overrideAttrs (oldAttrs: {
            pname = "go-warn-unused";
            patches = (oldAttrs.patches or []) ++ [
              ./patches/nounusederrors.patch
            ];
            meta = oldAttrs.meta // {
              description = "Go compiler with -nounusederrors flag";
            };
          });
        in {
          go = go-warn-unused;
        };
    };
}
