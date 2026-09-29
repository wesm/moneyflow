# Security

Moneyflow stores financial data on your machine and can send reviewed changes to supported
providers. Use a trusted account and device. File permissions and credential encryption do
not protect against a compromised operating system or a process running as your user.

## Data at rest

Go profiles use SQLite without application-level database encryption. Protect the profile
directory and backups; use full-disk encryption if appropriate. Transaction exports are
also unencrypted.

Credential storage differs by provider. The owning guides describe exact paths and recovery:

- [Monarch](docs/guide/monarch.md#where-are-credentials-stored): password-encrypted login vault
  and a separate owner-private session file.
- [YNAB](docs/guide/ynab.md#token-storage): owner-private token file, not password-encrypted.
- [SimpleFIN](docs/guide/simplefin.md#storage-and-recovery): owner-private Access URL file,
  not password-encrypted.
- [Amazon](docs/guide/amazon-mode.md): local exported files; no Amazon login credentials.

Do not put tokens, authenticator secrets, Access URLs, or passwords in shell arguments,
logs, screenshots, or issue reports. Profile backups can contain credentials.

## Network access

The web application has no built-in user authentication. Keep it on loopback or restrict
access through a trusted network and an authenticated TLS proxy. Canonical-origin checks
are not a replacement for authentication. See [web deployment](docs/config/advanced.md).

MCP HTTP is a separate loopback-only, bearer-authenticated endpoint. The token grants
access to the selected profile; `--allow-write` also registers mutation tools.
See [MCP transport and token handling](docs/guide/mcp.md#streamable-http).
Never publish query strings from either interface's access logs.

## Backups and upgrades

Stop every process using a profile before copying its complete directory. Keep database
sidecar files with it. An export is not a complete profile backup.

The preview schema is install-only: incompatible versions are refused rather than migrated.
Do not bypass this by editing schema metadata or deleting a live database.
Python data is not automatically imported or removed; see [moving to Go](docs/getting-started/transition.md).

## Report a vulnerability

Contact the maintainer privately at the public project contact address,
[info@wesmckinney.com](mailto:info@wesmckinney.com), rather than posting exploitable details in a public issue.
Include the version, affected boundary, and a synthetic reproduction when possible.
