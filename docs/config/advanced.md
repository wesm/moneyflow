# Paths and web access

Use `MONEYFLOW_HOME` to choose the profile catalog. Keep it separate from Python data.
Use `--profile` with an exact name or profile ID to bypass the selector.
See [storage](caching.md) for the directory layout and backups.

## Serve the browser application

```bash
moneyflow web --open=false
```

The default listener is loopback. The web application has no built-in user authentication.
For access from another machine, restrict the network or put an authenticated TLS reverse
proxy in front of it. Do not expose it directly to the public Internet.

For a path-preserving Caddy mount:

```bash
moneyflow web --open=false --listen 127.0.0.1:8080 --base-path /moneyflow/ --external-url https://moneyflow.example.invalid/moneyflow/
```

```caddyfile
moneyflow.example.invalid {
    handle /moneyflow/* {
        reverse_proxy 127.0.0.1:8080
    }
}
```

Replace the example host. Configure authentication or network access controls separately.
Preserve the full path; do not use a proxy rule that strips `/moneyflow/`.
With `--external-url`, mutations must use that canonical origin. Direct listener reads
remain available for diagnostics.

Omit query strings from proxy access logs: URLs can contain financial search and filter values.
[MCP HTTP](../guide/mcp.md#authenticated-http) has separate bearer-token and loopback requirements.

## What configuration is not carried forward?

Go does not read Python's category YAML, encrypted cache, or default-provider settings.
Use the profile selector and [transition guide](../getting-started/transition.md).
