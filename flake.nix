{
  inputs = {
    nixpkgs.url = "nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs, ... }@inputs:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = fn:
        nixpkgs.lib.genAttrs supportedSystems
        (system: fn { pkgs = import nixpkgs { inherit system; }; });
    in {
      devShells = forAllSystems ({ pkgs }: {
        default = pkgs.mkShell {
          nativeBuildInputs = with pkgs; [
            go_1_23
            protobuf
          ];
        };
      });
    };
}
