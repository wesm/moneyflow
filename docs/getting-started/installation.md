# Installation

Install Moneyflow as one binary, or build it from this checkout. This branch contains the Go replacement;
do not assume the latest published release already includes it. The old PyPI package installs Python Moneyflow.
See [moving to Go](transition.md) before replacing a Python installation.

## Available releases

The Go release pipeline builds standalone binaries for Linux, macOS, and Windows on AMD64 and ARM64.
Prebuilt binaries include the web application and need no Python, Go, or Bun runtime.

When a Go preview is available on [GitHub Releases](https://github.com/wesm/moneyflow/releases), use its tagged installer
below. Until then, [build from source](#build-from-source).

### Install a Tagged Preview

Replace `vX.Y.Z-rc.N` with an actual Go preview tag from the release page. Download and inspect the installer from that
same tag, then run it with `MONEYFLOW_VERSION` set. Preview releases are not selected by GitHub's `latest` release URL.

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

The default install directory is `~/.local/bin` on Linux/macOS and `%LOCALAPPDATA%\Programs\moneyflow\bin` on Windows.
Set `MONEYFLOW_INSTALL_DIR` before running the installer to choose another directory. Add that directory to your `PATH`
if needed. If Python moneyflow is also installed, check which executable your shell selects before running it.

To update, close Moneyflow and rerun the installer for the desired release. Installation replaces the binary only;
it does not remove profiles or resolve incompatible preview schemas. Keep a backup before opening data with a new version.

### Install the Latest Stable Go Release

Use these commands only after a stable Go release publishes the installer assets. Until then, use a tagged Go preview or
build from source. Leave `MONEYFLOW_VERSION` unset to select the latest stable release.

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

Here `<version>` is the release tag without its leading `v`. Download the archive for your system and `SHA256SUMS` from
the same release, verify the archive's SHA-256 checksum, then extract `moneyflow` or `moneyflow.exe` into a directory on
your `PATH`.

### Run the Go Application

```bash
moneyflow version
moneyflow tui --demo
moneyflow web --demo
moneyflow tui
```

Demo profiles are temporary and need no financial account. Persistent Go profiles live under `~/.moneyflow/v2` by default;
set `MONEYFLOW_HOME` to use a different catalog directory.

See [moving to Go](transition.md) for old Python data and incompatible Go preview profiles.

### Build from source

Install Go 1.27.1, Bun 1.3.14, and `make`, then build from the `go-port` branch:

```bash
git clone --branch go-port https://github.com/wesm/moneyflow.git
cd moneyflow
make web-install
make build
./bin/moneyflow tui --demo
```

Windows builds produce `bin/moneyflow.exe`. The build generates and embeds the web assets before compiling the binary.
See the [Go release guide](../development/releases.md) for the distribution workflow.
