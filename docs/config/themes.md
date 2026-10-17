# Themes

The terminal application (TUI) takes a color theme when it starts. The browser has its own
light, dark, and system modes. Neither reads the old Python `config.yaml` theme setting.

## Terminal

```bash
moneyflow tui --theme nord
moneyflow tui --demo --theme berg
```

Available names are `default`, `berg`, `nord`, `gruvbox`, `dracula`, `solarized-dark`, and
`monokai`. An unknown name stops the TUI with `unknown theme "NAME"`. The option applies to that
TUI process only.

The TUI reads its color depth from the environment:

| Environment | Colors |
| --- | --- |
| `NO_COLOR` set, to any value | None |
| `COLORTERM` contains `truecolor` or `24bit` | True color |
| `TERM` contains `256color` | 256 colors |
| Any other `TERM` | 16 colors |
| `TERM` empty or `dumb` | None |

`NO_COLOR` wins over the others.

## Browser

The theme button in the sidebar's footer cycles light, dark, and system modes. System follows
your operating system's setting. The browser starts dark on a first visit and remembers your
choice in browser storage. The TUI `--theme` flag does not affect it.
