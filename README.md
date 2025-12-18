# go-warn-unused

A Nix flake that patches the Go compiler to add `-nounusederrors` flag, treating unused variable/import errors as warnings.

## Usage

### In your flake.nix

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    go-warn-unused.url = "github:valeratrades/go-warn-unused";
  };

  outputs = { nixpkgs, go-warn-unused, ... }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs {
        inherit system;
        overlays = [ go-warn-unused.overlays.default ];
      };
    in {
      devShells.${system}.default = pkgs.mkShell {
        packages = [ pkgs.go ];
      };
    };
}
```

### Building/running with the flag

```sh
go build -gcflags="-nounusederrors" ./...
go run -gcflags="-nounusederrors" main.go
```

## How it works

This flake patches the Go compiler from nixpkgs to:
1. Add `NoUnusedErrors` flag to the compiler
2. Add `unusedf()` function that prints warnings instead of errors
3. Replace `softErrorf()` calls for unused vars/imports/labels with `unusedf()`

When `-nounusederrors` is passed, unused code produces warnings but compilation succeeds (exit 0).

## Updating

When nixpkgs updates Go, the patch should continue to work as long as the touched functions haven't changed significantly. If the patch fails to apply, it may need minor adjustments.
