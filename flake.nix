{
  description = "moneyflow - A powerful terminal UI for personal finance management";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-24.11";
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
            version = "0.5.3";
            format = "pyproject";

            src = ./.;

            nativeBuildInputs = with pythonPackages; [
              hatchling
            ];

            propagatedBuildInputs = with pythonPackages; [
              aiohttp
              click
              gql
              polars
              pyyaml
              textual
              cryptography
              python-dateutil
              # oathtool - pure Python TOTP generator (not in nixpkgs)
              (buildPythonPackage rec {
                pname = "oathtool";
                version = "2.3.1";
                pyproject = true;

                src = fetchPypi {
                  inherit pname version;
                  hash = "sha256-DfP22b9/cShz/fFETzPNWKa9W2h+0Eolar14OTrPLCU=";
                };

                build-system = [ setuptools setuptools-scm ];
                dependencies = [ autocommand path ];
              })
              # ynab - YNAB API client (not in nixpkgs) - using pre-built wheel
              (buildPythonPackage rec {
                pname = "ynab";
                version = "1.9.0";
                format = "wheel";

                src = pkgs.fetchurl {
                  url = "https://files.pythonhosted.org/packages/b2/9c/0ccd11bcdf7522fcb2823fcd7ffbb48e3164d72caaf3f920c7b068347175/ynab-1.9.0-py3-none-any.whl";
                  hash = "sha256-cqwCGWBbQoAUloTs0P7DvXXZOHctZc3uqbPmahsvRw0=";
                };

                dependencies = [
                  urllib3
                  python-dateutil
                  pydantic
                  typing-extensions
                  certifi
                ];
              })
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

            # Install package in editable mode if not already installed
            if ! python -c "import moneyflow" 2>/dev/null; then
              echo "Installing moneyflow in editable mode..."
              uv pip install -e . --quiet
            fi

            # Add uv-managed venv bin to PATH
            export PATH="$PWD/.venv/bin:$PATH"

            echo "Ready! Try: moneyflow --demo"
            echo "Run tests: pytest -v"
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
