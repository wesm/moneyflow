# Security

## Go v2 Preview

The `go-port` branch uses a separate `~/.moneyflow/v2` profile and does not read Python credential
state. Its SQLite database is not application-encrypted; use full-disk encryption and protect
profile copies and backups as financial data. SQLite files are created with owner-only platform
permissions, exact integer money, `synchronous=FULL`, and atomic revision-checked writes.

The Monarch read/refresh preview stores only session material at
`~/.moneyflow/v2/providers/monarch/session.json`. It never persists the Monarch email, password,
multifactor secret, or one-time code. The session file and its parent directories use owner-only
permissions and atomic replacement, but the session is still sensitive bearer material. Do not
copy it into logs, bug reports, fixtures, shell arguments, or repositories. Use:

```bash
moneyflow provider disconnect monarch
```

to remove only that session while preserving imported SQLite data. An expired session leaves the
profile browsable offline; reconnect through `moneyflow provider connect monarch`.

The embedded Go web server has no built-in user authentication. Keep its listener on loopback or
one explicit private/tailnet address, and use a trusted reverse proxy for TLS and any desired
authentication. When using `--external-url`, mutations are accepted only through that canonical
origin. Configure proxy access logs to omit query strings because URLs contain durable analytical
view state. Provider tokens, remote identifiers, labels, transaction contents, financial values,
and search text must never enter application logs.

Go v2 uses an install-only schema while the format stabilizes. It refuses incompatible profiles
instead of migrating them. Stop all moneyflow processes and move the complete v2 profile directory
aside before recreation; do not manipulate a live database or only one of its WAL-related files.

## Credential Storage

moneyflow provides secure credential storage to avoid storing plaintext passwords or requiring environment variables.

### How It Works

1. **Encryption**: Credentials are encrypted using Fernet (symmetric encryption with AES-128)
2. **Key Derivation**: Your encryption password is converted to a key using PBKDF2-HMAC-SHA256 with 100,000 iterations
3. **Salt**: A random 16-byte salt is generated per installation
4. **File Permissions**: Credential files are set to 0600 (readable only by owner)

### What's Stored

The `~/.moneyflow/credentials.enc` file contains:
- Monarch Money email address
- Monarch Money password
- TOTP/OTP secret for 2FA

### Why This Approach?

**Better than environment variables:**
- Environment variables can leak into shell history
- They can be accidentally committed to version control
- They're visible to other processes on the system

**Better than plaintext config files:**
- Credentials are encrypted at rest
- Requires password to decrypt
- Password is never written to disk

**Better than system keychains:**
- Portable across all platforms (Windows, macOS, Linux)
- No OS-specific dependencies
- Simple implementation

### Recommendations

1. **Use a strong encryption password**
   - At least 12 characters
   - Mix of letters, numbers, symbols
   - Don't reuse your Monarch password

2. **Protect your config directory**
   ```bash
   chmod 700 ~/.moneyflow
   chmod 600 ~/.moneyflow/*
   ```

3. **Backup your TOTP secret**
   - Store it securely (password manager, encrypted backup)
   - If you lose it, you'll need to reset 2FA on Monarch Money

4. **Delete credentials when done**
   ```bash
   rm ~/.moneyflow/credentials.enc
   rm ~/.moneyflow/salt
   ```

### Security Audit

The encryption implementation uses:
- `cryptography` library (widely audited, industry standard)
- Fernet (spec: https://github.com/fernet/spec)
- PBKDF2 with 100,000 iterations (OWASP minimum recommendation)
- SHA-256 hash function
- Random salt per installation

### Threat Model

**Protected against:**
- Casual file system access (files are encrypted)
- Accidental commits to git (credentials not in plaintext)
- Process inspection (credentials not in environment)

**Not protected against:**
- Attacker with your encryption password
- Memory dumps while TUI is running
- Keyloggers or screen capture
- Root/admin access to your system

### Reporting Security Issues

If you discover a security vulnerability, please email the maintainers directly rather than opening a public issue.
