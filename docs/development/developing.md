# Developing moneyflow

This guide covers the essential development workflow for contributing to moneyflow.

## Quick Start

```bash
# Clone repository
git clone https://github.com/wesm/moneyflow.git
cd moneyflow

# Install uv (if not already installed)
curl -LsSf https://astral.sh/uv/install.sh | sh

# Install dependencies (creates .venv)
uv sync

# Run the app in demo mode
uv run moneyflow --demo

# Run tests
uv run pytest -v

# Run type checker
uv run pyright moneyflow/
```

## Development Environment

### Standard Environment (uv)

**Required:**

- Python 3.11+
- [uv](https://docs.astral.sh/uv/) - Package manager

**Optional:**

- VS Code or PyCharm with Python extension

### Nix Environment (Alternative)

If you use Nix, you can set up a complete development environment with one command:

```bash
# Clone repository
git clone https://github.com/wesm/moneyflow.git
cd moneyflow

# Enter Nix development shell (includes Python, uv, and all dependencies)
nix develop

# Inside the Nix shell, use uv as normal
uv sync
uv run moneyflow --demo
uv run pytest -v
```

The Nix flake provides:

- Python 3.11 with all runtime dependencies
- Development tools: pytest, ruff, pyright
- uv for package management
- All dependencies pinned for reproducibility

**Benefits:**

- No need to install Python or uv separately
- Reproducible environment across machines
- Automatic cleanup when exiting the shell

**Run without entering shell:**

```bash
# Run tests directly
nix develop -c uv run pytest -v

# Run the app
nix develop -c uv run moneyflow --demo

# Or build and run the package
nix build
./result/bin/moneyflow --demo
```

## Development Workflow

### Working on Documentation

To preview documentation changes locally with live reload:

```bash
# Serve docs with live reload (auto-refreshes on file changes)
uv run mkdocs serve --livereload

# Then open http://127.0.0.1:8000 in your browser
# Edit files in docs/ and see changes instantly
```

**Note:** The `--livereload` flag is important - without it, changes won't auto-refresh in the browser.

### Before Starting Work

```bash
git pull
uv sync
uv run pytest -v  # Ensure clean starting point
```

### Making Changes

```bash
# Make your changes

# Run all code quality checks
uv run pytest -v                          # Tests must pass
uv run pyright moneyflow/                 # Type checking
uv run ruff format --check moneyflow/ tests/  # Code formatting
uv run ruff check moneyflow/ tests/       # Linting

# Auto-fix formatting and linting issues
uv run ruff format moneyflow/ tests/
uv run ruff check --fix moneyflow/ tests/

# Commit (only if all checks pass)
git add -A
git commit -m "your message"
```

### Running Tests

```bash
# All tests
uv run pytest -v

# Specific file
uv run pytest tests/test_data_manager.py -v

# Stop on first failure
uv run pytest -x

# With coverage
uv run pytest --cov --cov-report=html
open htmlcov/index.html
```

## CI/CD

Tests run automatically on every push and pull request:

- Python 3.11, 3.12 compatibility
- Full test suite
- Type checking with pyright

See `.github/workflows/test.yml` for details.

## Release Process

```bash
# 1. Bump version (runs all quality checks automatically)
./scripts/bump-version.sh 0.x.y

# 2. Review the version bump commit
git show

# 3. Push to GitHub
git push && git push --tags

# 4. Publish to PyPI (if authorized)
./scripts/publish-pypi.sh
```

The bump-version.sh script automatically:

- Runs all tests
- Runs type checking (pyright)
- Checks code formatting (ruff format)
- Runs linter (ruff check)
- Updates version in pyproject.toml and mkdocs.yml
- Updates uv.lock
- Creates commit and git tag

This ensures releases never have failing tests or code quality issues.

## Troubleshooting

**Tests fail after `git pull`:**

```bash
uv sync  # Sync dependencies
```

**Import errors or stale cache:**

```bash
# Clear Python cache
find . -type d -name __pycache__ -exec rm -rf {} +

# Reinstall dependencies
uv sync --reinstall
```

**Module not found errors:**

```bash
# Ensure you're using uv run
uv run pytest -v  # Correct
pytest -v         # Wrong - won't find modules
```

## Getting Help

- **Bugs**: [Open an issue](https://github.com/wesm/moneyflow/issues)
- **Questions**: [Start a discussion](https://github.com/wesm/moneyflow/discussions)
- **Contributing**: See [Contributing Guide](contributing.md)
