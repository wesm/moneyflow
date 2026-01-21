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

        # Custom packages not in nixpkgs
        oathtool = pythonPackages.buildPythonPackage rec {
          pname = "oathtool";
          version = "2.3.1";
          pyproject = true;

          src = pythonPackages.fetchPypi {
            inherit pname version;
            hash = "sha256-DfP22b9/cShz/fFETzPNWKa9W2h+0Eolar14OTrPLCU=";
          };

          build-system = with pythonPackages; [ setuptools setuptools-scm ];
          dependencies = with pythonPackages; [ autocommand path ];
        };

        ynab = pythonPackages.buildPythonPackage rec {
          pname = "ynab";
          version = "1.9.0";
          format = "wheel";

          src = pkgs.fetchurl {
            url = "https://files.pythonhosted.org/packages/b2/9c/0ccd11bcdf7522fcb2823fcd7ffbb48e3164d72caaf3f920c7b068347175/ynab-1.9.0-py3-none-any.whl";
            hash = "sha256-cqwCGWBbQoAUloTs0P7DvXXZOHctZc3uqbPmahsvRw0=";
          };

          dependencies = with pythonPackages; [
            urllib3
            python-dateutil
            pydantic
            typing-extensions
            certifi
          ];
        };

        httpx-sse = pythonPackages.buildPythonPackage rec {
          pname = "httpx-sse";
          version = "0.4.0";
          format = "wheel";

          src = pkgs.fetchurl {
            url = "https://files.pythonhosted.org/packages/e1/9b/a181f281f65d776426002f330c31849b86b31fc9d848db62e16f03ff739f/httpx_sse-0.4.0-py3-none-any.whl";
            hash = "sha256-8ymvbq5X6qK9/ZYrQlJHZK9oB16oc3Ci3pIK9TQeMY8=";
          };

          dependencies = with pythonPackages; [ httpx ];
        };

        pydantic-settings = pythonPackages.buildPythonPackage rec {
          pname = "pydantic-settings";
          version = "2.7.1";
          format = "wheel";

          src = pkgs.fetchurl {
            url = "https://files.pythonhosted.org/packages/b4/46/93416fdae86d40879714f72956ac14df9c7b76f7d41a4d68aa9f71a0028b/pydantic_settings-2.7.1-py3-none-any.whl";
            hash = "sha256-WQvp5uJNBtszpCYoKe3vaCUA7wCFZalpxz051fi/s/0=";
          };

          dependencies = with pythonPackages; [ pydantic python-dotenv ];
        };

        sse-starlette = pythonPackages.buildPythonPackage rec {
          pname = "sse-starlette";
          version = "2.2.1";
          format = "wheel";

          src = pkgs.fetchurl {
            url = "https://files.pythonhosted.org/packages/d9/e0/5b8bd393f27f4a62461c5cf2479c75a2cc2ffa330976f9f00f5f6e4f50eb/sse_starlette-2.2.1-py3-none-any.whl";
            hash = "sha256-ZBCj07oMiednXUwnOjAdZGScA6XvHKEB8QtH+JX9Dpk=";
          };

          dependencies = with pythonPackages; [ starlette anyio ];
        };

        typing-inspection = pythonPackages.buildPythonPackage rec {
          pname = "typing-inspection";
          version = "0.4.0";
          format = "wheel";

          src = pkgs.fetchurl {
            url = "https://files.pythonhosted.org/packages/31/08/aa4fdfb71f7de5176385bd9e90852eaf6b5d622735020ad600f2bab54385/typing_inspection-0.4.0-py3-none-any.whl";
            hash = "sha256-UOclWfzSpjZ6Gfen5hDmr8ufrJQMZQKQ7tiT1hOGgy8=";
          };

          dependencies = with pythonPackages; [ typing-extensions ];
        };

        mcp = pythonPackages.buildPythonPackage rec {
          pname = "mcp";
          version = "1.25.0";
          format = "wheel";

          src = pkgs.fetchurl {
            url = "https://files.pythonhosted.org/packages/e2/fc/6dc7659c2ae5ddf280477011f4213a74f806862856b796ef08f028e664bf/mcp-1.25.0-py3-none-any.whl";
            hash = "sha256-s3w4FEpmat0IYmFMx57Cdul9cqqMom1iKBjU4ni5cho=";
          };

          dependencies = with pythonPackages; [
            anyio
            httpx
            jsonschema
            pydantic
            pyjwt
            starlette
            typing-extensions
            uvicorn
            python-multipart
            python-dotenv
            typer
          ] ++ [
            httpx-sse
            pydantic-settings
            sse-starlette
            typing-inspection
          ];
        };
      in
      {
        packages = {
          default = pythonPackages.buildPythonApplication {
            pname = "moneyflow";
            version = "0.8.1";
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
            ] ++ [
              oathtool
              ynab
              mcp
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
