# Go releases

The Go application ships as one binary with its web application embedded. End users do not need
Python, Go, Bun, or a separate web server. Nix packaging has been removed.

A local build does not prove that a release was published or tested on every operating system.
Use prereleases while validating the replacement. Follow the
[transition policy](../getting-started/transition.md) before recommending a full switch;
provider reconnection and Amazon reimport alone do not preserve local-only edits.

SimpleFIN ships as **experimental**. Its automated adapter, storage, CLI, TUI, web, and MCP gates
are required; live-bank testing is pending and nonblocking for the preview. Request community
validation using the [SimpleFIN guide](../guide/simplefin.md#help-validate-an-institution).
Public-demo success is not bank validation. Maintainers must review institution evidence before
removing the experimental label; the label does not replace the automated release gates.

## Release contents

Every release contains six archives, `SHA256SUMS`, `install.sh`, and `install.ps1`:

| Platform | Architectures | Archive |
| --- | --- | --- |
| Linux | amd64, arm64 | `moneyflow_VERSION_linux_ARCH.tar.gz` |
| macOS | amd64, arm64 | `moneyflow_VERSION_darwin_ARCH.tar.gz` |
| Windows | amd64, arm64 | `moneyflow_VERSION_windows_ARCH.zip` |

`VERSION` is the tag without its leading `v`. Each archive contains only `moneyflow` or
`moneyflow.exe` at its root. Checksums cover the six archives and both installers. Builds disable
CGO and embed the version, source commit, and commit date. macOS and Windows binaries are not
publisher-signed or notarized; checksums detect download corruption, not publisher identity.

See [installation](../getting-started/installation.md) for installation and update instructions.
The installer replaces only the binary; it does not open or migrate profiles.

## Local rehearsal

Follow [development setup](developing.md) for the pinned mise tools and private temporary directory.
Also install Bash, Git, `tar`, `zip`, `unzip`, and `sha256sum`. The packaging command is intended
for Linux; CI runs it on Ubuntu. It cross-compiles all six targets without a C compiler.
Use an activated mise shell, or prefix each command below with `mise exec --`.

```bash
make web-install
make release-build RELEASE_VERSION=0.0.0-rc.0
```

Archives land in ignored `dist/release/`. The builder refuses to overwrite an existing directory;
use `RELEASE_DIR=dist/rehearsal-2` for another run. Nothing is tagged, pushed, or published.

Check an extracted binary on a matching host, for example Linux arm64:

```bash
mkdir -p dist/extracted
tar -xzf dist/release/moneyflow_0.0.0-rc.0_linux_arm64.tar.gz -C dist/extracted
MONEYFLOW_RELEASE_TEST_BINARY="$PWD/dist/extracted/moneyflow" \
  MONEYFLOW_RELEASE_TEST_VERSION=v0.0.0-rc.0 go test ./scripts -count=1 -v
MONEYFLOW_MCP_TEST_BINARY="$PWD/dist/extracted/moneyflow" \
  go test ./internal/mcp -run '^TestMCPSubprocessInteroperability$' -count=1 -v
```

The release smoke test checks version metadata and serves embedded HTML and JavaScript from the
extracted binary with temporary demo data. The MCP test checks reads, staged edits, commit, and
exports over stdio, plus HTTP token rotation, using an isolated synthetic profile. Installer tests
use loopback downloads and temporary installation directories, never the user's installed binary
or profiles.

## Publishing

`.github/workflows/release.yml` builds and checks archives on pull requests and manual workflow runs
without publishing. A pushed annotated tag triggers publication after all six native smoke jobs
pass. Stable tags must point to a commit on `main`; preview tags may be cut from `go-port`.

Use `vX.Y.Z` for a stable release or `vX.Y.Z-rc.N` for a preview. Choose the version deliberately;
there is no Python version bump for Go releases. The annotated tag message becomes the release
notes. Include provider limitations and data-format changes in those notes.

After reviewing a clean, verified commit and obtaining approval to publish:

```bash
# Replace VERSION with the chosen version, such as X.Y.Z-rc.N.
# Write reviewed release notes to a local file outside tracked source.
git tag -a vVERSION -F /path/to/release-notes.md
git push origin refs/tags/vVERSION
```

The workflow uploads assets to a draft release, then publishes it. Preview tags are marked
prerelease and do not replace the latest stable release. No PyPI credentials or cross-repository
tokens are needed. Only the release publication and documentation deployment jobs receive
repository-content write permission.

If upload or publication fails, inspect the workflow and any draft release before retrying. The
workflow does not overwrite an existing release. Do not delete or move an already-published tag to
retry a build; use a new version when the source changes.

After publication, install from the tagged assets on a clean machine and confirm `moneyflow version`,
`moneyflow tui --demo`, and `moneyflow web --demo`. The actual GitHub publication and platform UI
checks cannot be verified by a local cross-build alone.

## Documentation and retired release tools

The stable release workflow calls `.github/workflows/docs.yml` with the published tag.
That workflow builds and deploys the Markdown from that tag. Prereleases leave the public
site unchanged. A manual docs deployment requires a stable tag; it does not deploy the
working branch. See [documentation maintenance](https://github.com/wesm/moneyflow/blob/main/docs/README.md).

The Python application and its PyPI publishing scripts are retired from this checkout.
Do not update the old `stable` branch or publish a Python package to release Go.
Historical Python versions remain in existing releases and Git history.

Before deleting an old installed application or profile, follow
[moving to Go](../getting-started/transition.md). Publication never authorizes deleting user data.
