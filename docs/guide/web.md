# Web interface

Run `./bin/moneyflow web` to open the profile selector. Use `--demo` for a disposable synthetic
profile, or `--profile "Example Profile"` to select one explicitly. The browser and TUI share
profile data and server-authoritative transitions.

Keyboard-driven refinement remains central: grouping, drill/back, search, selection, edits and
review use the [shared workflows](navigation.md). Text fields and overlays own their keys while
focused. Browser navigation retains analytical view state, not transient selection.

## Private reverse proxy

For a private proxy serving `https://finance.example.com/moneyflow/`:

```bash
./bin/moneyflow web --listen 127.0.0.1:8080 --open=false \
  --base-path /moneyflow --external-url https://finance.example.com/moneyflow
```

Preserve the prefix when proxying to the listener; do not use a proxy rule that strips it.
For example, the Caddy site routes can include:

```caddyfile
redir /moneyflow /moneyflow/ 308
handle /moneyflow/* {
    reverse_proxy 127.0.0.1:8080
}
```

The external URL's path must match the base path. Canonical-origin mutation checks mean direct
listener access is not an alternate write origin when an external URL is configured.

The financial web app has no built-in user authentication. Keep it on a trusted private network,
or put access control in the proxy. A mutation token does not make publicly readable financial
data private. Do not expose it to the public internet merely because the documentation site is public.

## Reconnect and recovery

A profile may need reconnect, vault unlock or explicit schema recovery. Use the displayed workflow.
Closing a tab does not discard durable pending work. If a mutation sees a stale revision, inspect
the new state and invoke it again deliberately. Provider write batches have their own status and
recovery controls; see [editing](editing.md).
