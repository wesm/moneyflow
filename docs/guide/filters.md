# Filters and time

Press `f` to inspect the available analytical filters and `/` to search. These project the whole
matching result, not only the visible window. Hidden-state filtering changes what you see; it
does not change the provider's hidden flag.

TUI startup can set a time range:

```bash
./bin/moneyflow tui --profile "Example Profile" --year 2026
./bin/moneyflow tui --demo --mtd
```

Use the current command's `--help` for mutually exclusive time options. In the view, `t` changes
time granularity, Enter drills into a period, Left/Right move between periods and `a` clears
time selection.

Search in TUI/web is case-insensitive regular-expression matching of merchant/category names.
[MCP search](mcp.md) uses literal matching and can include notes; do not translate a browser
regular expression into an MCP search expecting the same interpretation.

Filtered exports use these analytical predicates against **committed** rows. A pending hide,
rename, recategorization or deletion can therefore make an export differ from the visible view.
See [export](export.md) before using it for reconciliation.
