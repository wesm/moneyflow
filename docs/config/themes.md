# Themes

Choose a TUI theme when starting Moneyflow:

```bash
moneyflow tui --theme nord
moneyflow tui --demo --theme berg
```

Available names are `default`, `berg`, `nord`, `gruvbox`, `dracula`,
`solarized-dark`, and `monokai`. An unknown name is rejected.

The option applies to that TUI process. Go does not read the old Python `config.yaml`
theme setting. Browser appearance is controlled by the web application's theme controls,
not the TUI flag.
