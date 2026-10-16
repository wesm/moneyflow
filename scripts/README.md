# Scripts

- `release-build.sh` builds the six Go archives and checksums without publishing.
- `install.sh` installs a selected release on Linux or macOS.
- `install.ps1` installs a selected release on Windows.
- `dev-profile.sh` seeds the synthetic `make dev` profile when it is missing.
- `dev.sh` runs the `make dev` servers. See
  [live development](../docs/development/developing.md#live-development).
- `views-host-check.sh` builds a sample host against a Git archive of HEAD, with the Vite
  configuration and the workspace example from `docs/development/embedding.md`.

See [releases](../docs/development/releases.md) for packaging and publication.
The Go tests in this directory exercise extracted binaries and installers with temporary
directories and local download servers. They do not replace the user's installed application.

Documentation no longer runs the Python screenshot generator. See
[documentation maintenance](../docs/README.md) for visual assets and site builds.

`make docs-check` builds and checks the combined site through `docs/tools/site.ts`.
