{
  description = "lightwalletd - Zcash light wallet server";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
  }:
    flake-utils.lib.eachDefaultSystem (system: let
      pkgs = nixpkgs.legacyPackages.${system};
    in {
      devShells.default = pkgs.mkShell {
        buildInputs = with pkgs; [
          go_1_25
          gnumake
          git

          # Protobuf toolchain
          protobuf
          protoc-gen-go
          protoc-gen-go-grpc
        ];
      };
    });
}
