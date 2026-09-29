!!! warning "Legacy Python 0.11.1"
    Frozen documentation, not the current application. Provider APIs may have changed.
    Use the [current documentation](/docs/) for Go and read the
    [transition guidance](/docs/getting-started/python-transition/) before changing profiles.

# Themes

moneyflow includes 7 color themes to customize the TUI appearance.

## Available Themes

### Default

Original moneyflow look and feel using Textual's built-in dark theme colors.

[Screenshot omitted: this release's source archive does not include generated images.]

### Berg

Warm amber aesthetic inspired by classic 1980s financial terminals. Muted earth tones for comfortable extended viewing.

[Screenshot omitted: this release's source archive does not include generated images.]

### Nord

Arctic blue palette popular among developers. Cool tones designed for eye-friendly extended sessions.

[Screenshot omitted: this release's source archive does not include generated images.]

### Gruvbox

Retro warm color scheme with vintage appeal. Beloved by terminal enthusiasts for its comfortable warm tones.

[Screenshot omitted: this release's source archive does not include generated images.]

### Dracula

Modern dark theme with purple accents. Popular across editors and terminals.

[Screenshot omitted: this release's source archive does not include generated images.]

### Monokai

Classic Sublime Text color scheme with vibrant syntax highlighting and warm earth tones.

[Screenshot omitted: this release's source archive does not include generated images.]

### Solarized Dark

Scientifically designed precision color scheme by Ethan Schoonover. Optimized to reduce eye strain.

[Screenshot omitted: this release's source archive does not include generated images.]

## Configuration

### Set Default Theme

Create or edit `~/.moneyflow/config.yaml`:

```yaml
version: 1

settings:
  theme: berg  # or nord, gruvbox, dracula, solarized-dark, monokai, default
```

Restart moneyflow for the theme to take effect.

### Temporary Override

Use the `--theme` option to temporarily try a different theme without changing your config:

```bash
# Try the berg theme
uvx --from 'moneyflow==0.11.1' moneyflow --theme berg

# Use nord theme for this session only
uvx --from 'moneyflow==0.11.1' moneyflow --mtd --theme nord

# Test dracula theme
uvx --from 'moneyflow==0.11.1' moneyflow --demo --theme dracula
```

The override only applies to the current session and doesn't modify `config.yaml`.

### Invalid Theme Names

If you specify an invalid theme, moneyflow will show an error with the list of available themes:

```bash
$ moneyflow --theme foobar
Error: Invalid value for '--theme': 'foobar' is not one of
'default', 'berg', 'nord', 'gruvbox', 'dracula', 'solarized-dark', 'monokai'.
```
