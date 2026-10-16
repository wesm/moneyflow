# Installation

Moneyflow is one binary with the web application built in. Install a tagged Go preview, or
build it from this checkout. Do not assume the latest published release already includes the
Go application. The old PyPI package installs Python Moneyflow.
Read [moving to Go](transition.md) before you replace a Python installation.

## Available releases

The Go release pipeline builds standalone binaries for Linux, macOS, and Windows on AMD64 and
ARM64. Prebuilt binaries include the web application and need no Python, Go, or Bun runtime.

When a Go preview is available on [GitHub Releases](https://github.com/wesm/moneyflow/releases),
use its tagged installer below. Until then, [build from source](#build-from-source).

### Install a Tagged Preview

Replace `vX.Y.Z-rc.N` with a real Go preview tag from the release page. Download the installer
from that same tag, inspect it, then run it with `MONEYFLOW_VERSION` set. GitHub's `latest`
release URL never selects a preview.

=== "Linux / macOS"

    ```bash
    MONEYFLOW_VERSION=vX.Y.Z-rc.N
    curl -fsSL "https://github.com/wesm/moneyflow/releases/download/${MONEYFLOW_VERSION}/install.sh" -o install.sh
    less install.sh
    MONEYFLOW_VERSION="$MONEYFLOW_VERSION" sh install.sh
    ```

=== "Windows PowerShell"

    ```powershell
    $env:MONEYFLOW_VERSION = 'vX.Y.Z-rc.N'
    Invoke-WebRequest "https://github.com/wesm/moneyflow/releases/download/$env:MONEYFLOW_VERSION/install.ps1" -OutFile install.ps1
    Get-Content .\install.ps1
    .\install.ps1
    ```

The installer puts the binary in `~/.local/bin` on Linux and macOS, and in
`%LOCALAPPDATA%\Programs\moneyflow\bin` on Windows. Set `MONEYFLOW_INSTALL_DIR` before running
it to choose another directory. Add that directory to your `PATH` if needed. If Python
Moneyflow is also installed, check which executable your shell selects before you run it.

To update, close Moneyflow and rerun the installer for the release you want. The installer
replaces only the binary. It does not remove profiles or resolve incompatible preview schemas.
Keep a backup before you open data with a new version.

### Install the Latest Stable Go Release

These commands work only after a stable Go release publishes its installer assets. Until then,
use a tagged Go preview or build from source. Leave `MONEYFLOW_VERSION` unset to select the
latest stable release.

=== "Linux / macOS"

    ```bash
    curl -fsSL https://github.com/wesm/moneyflow/releases/latest/download/install.sh -o install.sh
    less install.sh
    sh install.sh
    ```

=== "Windows PowerShell"

    ```powershell
    Invoke-WebRequest https://github.com/wesm/moneyflow/releases/latest/download/install.ps1 -OutFile install.ps1
    Get-Content .\install.ps1
    .\install.ps1
    ```

### Manual Download

Each Go release includes `SHA256SUMS`, `install.sh`, `install.ps1`, and these six archives:

| Operating system | Architecture | Archive |
| --- | --- | --- |
| Linux | AMD64 | `moneyflow_<version>_linux_amd64.tar.gz` |
| Linux | ARM64 | `moneyflow_<version>_linux_arm64.tar.gz` |
| macOS | AMD64 | `moneyflow_<version>_darwin_amd64.tar.gz` |
| macOS | ARM64 | `moneyflow_<version>_darwin_arm64.tar.gz` |
| Windows | AMD64 | `moneyflow_<version>_windows_amd64.zip` |
| Windows | ARM64 | `moneyflow_<version>_windows_arm64.zip` |

`<version>` is the release tag without its leading `v`. Download your archive and
`SHA256SUMS` from the same release and verify the archive's SHA-256 checksum. Then extract
`moneyflow` or `moneyflow.exe` into a directory on your `PATH`.

### Run the Go Application

```bash
moneyflow version
moneyflow web --demo
moneyflow tui --demo
moneyflow web
moneyflow tui
```

Demo profiles are temporary and need no financial account. Saved Go profiles live under
`~/.moneyflow` by default. Set `MONEYFLOW_HOME` to use a different catalog directory.
Moneyflow refuses a folder that holds Python app data.

See [moving to Go](transition.md) for old Python data and incompatible Go preview profiles.

### Build from source

Install [mise](https://mise.jdx.dev/getting-started.html) and `make`. Then clone the default
`main` branch and build:

```bash
git clone https://github.com/wesm/moneyflow.git
cd moneyflow
mise trust
mise install
mise exec -- make web-install build
```

The build generates the web assets, embeds them, and compiles `bin/moneyflow`, or
`bin/moneyflow.exe` on Windows. Follow [development setup](../development/developing.md)
before running tests or temporary demos on Unix. The [Go release guide](../development/releases.md)
covers the distribution workflow.
