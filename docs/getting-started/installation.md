# Installation

Moneyflow is one binary with the web app built in. It runs on Linux, macOS, and Windows, on
AMD64 and ARM64. It needs no Python, Go, or Bun runtime.

## Install the latest release

The installer downloads the binary for your system from
[GitHub Releases](https://github.com/wesm/moneyflow/releases) and checks it against the
release's `SHA256SUMS`. Download it, read it, then run it.

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

The binary goes in `~/.local/bin` on Linux and macOS, and in
`%LOCALAPPDATA%\Programs\moneyflow\bin` on Windows. Set `MONEYFLOW_INSTALL_DIR` before you run
the installer to choose another directory. Add the directory to your `PATH` if needed.

To install a specific release, set `MONEYFLOW_VERSION` to its tag, for example
`MONEYFLOW_VERSION=vX.Y.Z sh install.sh`. Download the installer from that same release.

If Python Moneyflow is also installed, check which `moneyflow` your shell runs:
`moneyflow version` prints the Go version.

## Check that it works

```bash
moneyflow version
moneyflow web --demo
moneyflow tui --demo
```

The demo needs no account and is deleted when you quit. Next, follow the
[quick start](quickstart.md).

## Update

Close Moneyflow and run the installer again. It replaces only the binary. It does not open,
change, or remove your profiles.

## Download manually

Each release includes `SHA256SUMS`, `install.sh`, `install.ps1`, and these archives:

| Operating system | Architecture | Archive |
| --- | --- | --- |
| Linux | AMD64 | `moneyflow_<version>_linux_amd64.tar.gz` |
| Linux | ARM64 | `moneyflow_<version>_linux_arm64.tar.gz` |
| macOS | AMD64 | `moneyflow_<version>_darwin_amd64.tar.gz` |
| macOS | ARM64 | `moneyflow_<version>_darwin_arm64.tar.gz` |
| Windows | AMD64 | `moneyflow_<version>_windows_amd64.zip` |
| Windows | ARM64 | `moneyflow_<version>_windows_arm64.zip` |

`<version>` is the release tag without its leading `v`.

1. Download your archive and `SHA256SUMS` from the same release.
2. Check the archive's SHA-256 checksum against `SHA256SUMS`.
3. Extract `moneyflow` or `moneyflow.exe` into a directory on your `PATH`.

macOS and Windows binaries are not signed or notarized. The checksums detect a damaged
download; they do not prove who published it.

## Where your data lives

Profiles live in `~/.moneyflow`. Set `MONEYFLOW_HOME` to use another directory. Moneyflow
refuses a directory that holds Python Moneyflow data, so your Python files are never touched.
See [moving to Go](transition.md).

## Build from source

Install [mise](https://mise.jdx.dev/getting-started.html) and `make`, then:

```bash
git clone https://github.com/wesm/moneyflow.git
cd moneyflow
mise trust
mise install
mise exec -- make web-install build
```

This builds the web app, embeds it, and writes `bin/moneyflow` (`bin/moneyflow.exe` on
Windows). Read [development setup](../development/developing.md) before you run the tests.
