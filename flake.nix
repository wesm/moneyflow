{
  description = "moneyflow - A powerful terminal UI for personal finance management";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        python = pkgs.python311;
        pythonPackages = python.pkgs;
      in
      {
        packages = {
          default = pythonPackages.buildPythonApplication {
            pname = "moneyflow";
            version = "0.5.2";
            format = "pyproject";

            src = ./.;

            nativeBuildInputs = with pythonPackages; [
              hatchling
            ];

            propagatedBuildInputs = with pythonPackages; [
              aiohttp
              click
              gql
              oathtool
              polars
              pyyaml
              textual
              cryptography
              python-dateutil
            ];

            # Skip tests during build (can be run separately)
            doCheck = false;

            # Optional: run tests if dependencies are available
            nativeCheckInputs = with pythonPackages; [
              pytest
              pytest-asyncio
              pytest-cov
              pytest-mock
            ];

            meta = with pkgs.lib; {
              description = "Track your moneyflow - A powerful terminal UI for personal finance management";
              homepage = "https://github.com/wesm/moneyflow";
              license = licenses.mit;
              maintainers = [ ];
              mainProgram = "moneyflow";
              platforms = platforms.all;
            };
          };
        };

        # Development shell with all dev dependencies
        devShells.default = pkgs.mkShell {
          buildInputs = [
            python
            pythonPackages.hatchling
            pythonPackages.aiohttp
            pythonPackages.click
            pythonPackages.gql
            pythonPackages.oathtool
            pythonPackages.polars
            pythonPackages.pyyaml
            pythonPackages.textual
            pythonPackages.cryptography
            pythonPackages.python-dateutil
            # Dev dependencies
            pythonPackages.pytest
            pythonPackages.pytest-asyncio
            pythonPackages.pytest-cov
            pythonPackages.pytest-mock
            pythonPackages.ruff
            pkgs.pyright
            # uv for development workflow
            pkgs.uv
          ];

          shellHook = ''
            echo "moneyflow development environment"
            echo "Run 'uv sync' to set up the project"
            echo "Run 'uv run moneyflow' to start the application"
            echo "Run 'uv run pytest' to run tests"
          '';
        };

        # Alias for the main package
        apps.default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/moneyflow";
        };
      }
    );
}
