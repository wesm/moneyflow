# AGENTS.md - moneyflow Development Guide

## CRITICAL: Git Branch Management for AI Assistants

**⚠️ NEVER change git branches, pull, or create new branches without explicit user permission.**

- ✅ **ALWAYS ask before** `git checkout <branch>`
- ✅ **ALWAYS ask before** `git pull` or `git fetch` followed by merge/rebase
- ✅ **ALWAYS ask before** creating new branches
- ✅ **Stay on the branch the user checked out** unless they explicitly ask you to switch
- ❌ **NEVER run `git checkout` on your own**
- ❌ **NEVER run `git pull` on your own**
- ❌ **NEVER create branches autonomously**
- ❌ **NEVER switch branches when starting a new task** - the user has already set up the branch

**If you need to work on a different branch**, ask the user first:
- "Should I switch to branch X to work on this?"
- "Should I create a new branch for this feature?"

**When the user says a PR is merged**, do NOT automatically checkout main and pull. Wait for explicit instructions.

## CRITICAL: Personal Data Protection

**⚠️ NEVER include user's personal data in code, comments, or documentation.**

This is a personal finance application. Users may share screenshots or logs containing real financial data (account names, transaction details, merchant names, etc.) when debugging issues.

- ❌ **NEVER copy personal data** from screenshots/logs into code comments
- ❌ **NEVER use real account names, card numbers, or transaction details** as examples
- ✅ **Use generic examples** like "Account Name", "Example Merchant", etc.
- ✅ **If you need to reference data formats**, use clearly fake data

## Project Overview

moneyflow is a terminal-based UI for power users to manage personal finance transactions efficiently. Built with Python using Textual for the UI and Polars for data processing. Supports multiple backends including Monarch Money, with more platforms planned (YNAB, Lunch Money, etc.).

## Development Setup

### Using uv (REQUIRED)

**IMPORTANT**: This project uses **uv** exclusively for all development workflows. Always use `uv run` for executing scripts. Never use pip, pipenv, poetry, or other package managers.

**CRITICAL FOR AI ASSISTANTS (Codex, Claude Code, etc.)**:
- ❌ **NEVER run `pip install` or `uv pip install` to modify the user's environment**
- ❌ **NEVER run `uv tool install` for project dependencies**
- ✅ All dependencies MUST be declared in `pyproject.toml` and installed via `uv sync`
- ✅ Use `uv run <command>` to run tools in the project's virtual environment
- 💡 This ensures **reproducibility** - anyone can clone the repo and run `uv sync` to get the exact same environment

```bash
# Install uv if not already installed
curl -LsSf https://astral.sh/uv/install.sh | sh

# FIRST TIME SETUP: Sync dependencies (includes dev dependencies for testing)
uv sync

# This creates a virtual environment and installs all dependencies
# You MUST run this before running tests or the TUI for the first time

# After sync, run the TUI
uv run moneyflow

# Run tests (ALWAYS before committing)
uv run pytest

# Run tests with coverage
uv run pytest --cov --cov-report=html

# View coverage report
open htmlcov/index.html
```

**If you get `ModuleNotFoundError`**: Run `uv sync` first!

### Test-Driven Development (CRITICAL)

**This project handles financial data. We cannot afford slip-ups.**

**MANDATORY WORKFLOW**:
1. **Write tests first** for any new feature or bug fix
2. **Run tests** - verify they fail as expected
3. **Implement** the feature/fix
4. **Run tests again** - verify all tests pass
5. **Check coverage** - ensure new code is tested
6. **Only commit when tests are green**

**Before EVERY commit**:
```bash
# Run full test suite
uv run pytest -v

# Run type checker
uv run pyright moneyflow/

# Check coverage
uv run pytest --cov --cov-report=term-missing

# Check markdown formatting (if docs changed)
markdownlint --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

**All tests must pass, type checking must be clean, and markdown must be properly formatted before committing.** No exceptions.

### Project Structure

**IMPORTANT**: All Python source code must be in the `moneyflow/` package. No Python files should live at the top level.

```
moneyflow/
├── moneyflow/                   # Main package (ALL code goes here)
│   ├── backends/                # Backend implementations
│   │   ├── base.py              # Backend protocol/base types
│   │   ├── monarch.py           # Monarch backend adapter
│   │   ├── monarch_client.py    # Vendored GraphQL client (keep separate for upstream diffs)
│   │   ├── ynab.py              # YNAB backend adapter
│   │   ├── ynab_client.py       # YNAB API client wrapper
│   │   ├── amazon.py            # Amazon order data backend
│   │   └── demo.py              # Demo backend
│   ├── data/                    # Business/data layer
│   │   ├── data_manager.py      # Data orchestration with Polars
│   │   ├── state.py             # App state management
│   │   ├── cache_manager.py     # Core cache persistence
│   │   ├── cache_orchestrator.py # Cache flow orchestration
│   │   ├── credentials.py       # Encrypted credential storage
│   │   ├── duplicate_detector.py # Duplicate detection
│   │   ├── commit_orchestrator.py # DataFrame update logic
│   │   ├── time_navigator.py    # Time period calculations
│   │   └── categories.py        # Category normalization/config
│   ├── importers/               # File importers
│   ├── mcp/                     # MCP server entrypoint and tools
│   ├── tui/                     # Textual UI layer
│   │   ├── app.py               # Main Textual application
│   │   ├── app_controller.py    # UI orchestration
│   │   ├── formatters.py        # UI formatting/presentation helpers
│   │   ├── keybindings.py       # Keyboard shortcut definitions
│   │   ├── screens/             # UI screens and modals
│   │   ├── widgets/             # Custom UI widgets
│   │   └── styles/              # Textual CSS
│   ├── cli.py                   # CLI entrypoint
│   └── version.py               # Version metadata helpers
├── tests/                       # Test suite
│   ├── conftest.py              # Pytest fixtures
│   ├── mock_backend.py          # Mock finance backend
│   ├── test_state.py            # State management tests
│   ├── test_data_manager.py     # Data operations tests
│   ├── test_formatters.py       # UI formatting/presentation tests
│   ├── test_time_navigator.py   # Time navigation tests
│   ├── test_commit_orchestrator.py # DataFrame update tests
│   ├── screens/                 # Screen-level UI tests
│   ├── integration/             # Integration/Textual smoke tests
│   └── test_workflows.py        # Edit workflow tests
├── pyproject.toml               # Project metadata and dependencies
├── README.md                    # User documentation
├── AGENTS.md                    # Canonical AI assistant development guide
└── CLAUDE.md                    # Symlink to AGENTS.md for Claude Code
```

**File Organization Rules**:
- ✅ All business logic in `moneyflow/` package
- ✅ All tests in `tests/` directory
- ✅ Entry point via `moneyflow` command (configured in pyproject.toml)
- ❌ No `.py` files at top level
- ❌ No duplicate files between top-level and package

## Testing Strategy

**IMPORTANT**: All business logic must be tested before running against real data.

### Testing Architecture

1. **Mock Backend**: `tests/mock_backend.py` provides a `MockMonarchMoney` class that simulates the API without making real network calls.

2. **Test Fixtures**: `tests/conftest.py` provides reusable test data and fixtures.

3. **Separation of Concerns**:
   - `moneyflow/data/state.py`: Pure state management (no I/O) - easily testable
   - `moneyflow/data/data_manager.py`: Takes backend instances via dependency injection - can use mocks
   - `moneyflow/tui/`: Testable with Textual pilot tests

### What We Test

- ✅ State management: undo/redo, change tracking
- ✅ Data operations: aggregation, filtering, search
- ✅ Edit workflows: merchant rename, category change, hide toggle
- ✅ Bulk operations: multi-select, bulk edit
- ✅ Duplicate detection: finding and handling duplicates
- ✅ **Presentation logic**: View formatting, flag computation (100% coverage)
- ✅ **Time navigation**: Date calculations, leap years, boundaries (100% coverage)
- ✅ **DataFrame updates**: Critical commit logic (100% coverage)
- ✅ Edge cases: empty datasets, invalid data, API failures

### Running Tests

**ALWAYS use `uv run` for running tests:**

```bash
# Run all tests (run before EVERY commit)
uv run pytest -v

# Run with coverage report
uv run pytest --cov --cov-report=html --cov-report=term-missing

# Run specific test file
uv run pytest tests/test_state.py -v

# Run tests matching a pattern
uv run pytest -k "test_undo" -v

# Run and stop on first failure
uv run pytest -x

# Run and show local variables on failure
uv run pytest -l
```

### Coverage Requirements

**Business Logic Coverage Target: >90%**

Core modules must maintain high coverage:
- `moneyflow/data/state.py`: State management
- `moneyflow/data/data_manager.py`: Data operations and backend integration
- `moneyflow/data/duplicate_detector.py`: Duplicate detection
- `moneyflow/data/cache_manager.py`: Core cache persistence
- `moneyflow/data/cache_orchestrator.py`: Cache orchestration
- `moneyflow/data/time_navigator.py`: Time period calculations
- `moneyflow/data/commit_orchestrator.py`: DataFrame update logic
- `moneyflow/tui/formatters.py`: UI formatting and presentation helpers

UI layer coverage is less critical but still valuable.

View coverage report:
```bash
uv run pytest --cov --cov-report=html
open htmlcov/index.html
```

### Test-Driven Development Workflow

1. Write tests first for new features
2. Run tests to verify they fail
3. Implement the feature
4. Run tests to verify they pass
5. Refactor while keeping tests green

## Code Quality Checks

**CRITICAL**: All code quality checks MUST pass before committing. This ensures consistent code quality and prevents regressions.

### Required Checks (run before EVERY commit)

```bash
# 1. Run full test suite
uv run pytest -v

# 2. Type checking (pyright)
uv run pyright moneyflow/

# 3. Code formatting (ruff format)
uv run ruff format --check moneyflow/ tests/

# 4. Linting (ruff check)
uv run ruff check moneyflow/ tests/

# 5. Markdown formatting (if docs changed)
markdownlint --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

**All checks must pass with zero errors** before creating a commit or release.

**Note:** Markdown checks (5) only need to run if you've modified documentation files (README.md or docs/).

### Auto-Fixing Issues

```bash
# Auto-format code
uv run ruff format moneyflow/ tests/

# Auto-fix linting issues
uv run ruff check --fix moneyflow/ tests/
```

### Working with Documentation

The project uses [Zensical](https://zensical.org) (modern theme) for documentation.
Zensical is built by the Material for MkDocs team and reads the existing `mkdocs.yml`
configuration natively.

**Starting the docs server:**

```bash
# Serve docs locally with live reload (default: http://localhost:8000)
uv run zensical serve
```

**Building the site (no server):**

```bash
uv run zensical build   # output written to site/
```

**Generating/regenerating screenshots:**

```bash
# Generate all screenshots
uv run python scripts/generate_screenshots.py

# Generate only specific screenshots (by filename filter)
uv run python scripts/generate_screenshots.py --filter amazon-matching

# IMPORTANT: After regenerating, restart the docs server for changes to appear
```

**Known Issues:**

- **Stale screenshots/images**: If docs show old images after regenerating, delete the
  `site/` directory and rebuild, then hard refresh the browser (`Cmd+Shift+R` /
  `Ctrl+Shift+R`).

- **HTML img tags need different paths**: When using `<img>` tags in markdown (for tables),
  paths resolve relative to the page URL, not the source file. Use `../../assets/` for
  pages in subdirectories like `guide/navigation.md`.

### Configuration

- `pyproject.toml` contains configuration for ruff and pyright
- `moneyflow/backends/monarch_client.py` is excluded from ruff/pyright checks (external vendor code)
- Line length: 100 characters
- Target Python version: 3.11

## Code Style

- **Use type hints** for all function signatures
- **No inline imports**: All imports must be at the top of the file, not inside functions/methods
  - Inline imports are slower (import happens on every call)
  - Harder to see dependencies at a glance
  - Exception: Circular import issues (rare)
- **Document complex logic** with comments explaining "why", not "what"
- **Keep functions focused** - Single responsibility, easy to test
- **Use meaningful variable names** - Prefer clarity over brevity

## Making Changes to monarch_client.py

The `moneyflow/backends/monarch_client.py` file is kept separate to make it easy
to generate diffs for upstream contributions to `hammem/monarchmoney`:

```bash
# Generate a diff against the original
diff moneyflow/backends/monarch_client.py /path/to/original/monarchmoney.py > my_changes.patch
```

## Security Notes

- Credentials are encrypted with Fernet (AES-128)
- Never commit `.mm/` directory (session data)
- Never commit `~/.moneyflow/` directory (encrypted credentials)
- Never commit test data with real credentials
- See SECURITY.md for full security documentation

## Common Tasks

### Adding a New Feature

1. Create tests in `tests/test_*.py`
2. Implement in appropriate module
3. Update keyboard shortcuts in `moneyflow/tui/keybindings.py`
4. Update README.md with new functionality
5. Run full test suite

### Debugging

```bash
# Enable Textual dev tools
uv run textual console

# Then in another terminal
uv run moneyflow

# View logs in the console
```

### Updating Dependencies

```bash
# Add new dependency to pyproject.toml manually, then:
uv sync

# Or add directly
uv add package-name

# Update all dependencies
uv lock --upgrade
uv sync
```

## Git Workflow

**CRITICAL**: Never commit without running all code quality checks first!

**IMPORTANT**: When working with Codex, Claude Code, or other AI assistants:
- ✅ AI can create commits locally
- ❌ AI must NEVER push to git without explicit user permission
- ❌ AI must NEVER create new branches unless explicitly asked by the user
- ❌ AI must NEVER amend commits unless explicitly asked by the user
- 💡 User should review commits before pushing

**Pull request descriptions**:
- Do not include a "Test Plan", "Verification", or similar checklist/section in PR descriptions.
- Keep PR descriptions focused on the change summary and useful context.
- Report verification results in chat/status updates instead.

```bash
# MANDATORY: Run all code quality checks before committing
uv run pytest -v                          # All tests must pass
uv run pyright moneyflow/                 # Type checking must be clean
uv run ruff format --check moneyflow/ tests/  # Code must be formatted
uv run ruff check moneyflow/ tests/       # Linting must pass

# Only if ALL checks pass, then commit
git add -A
git commit -m "Descriptive commit message"

# WAIT for user approval before pushing
# git push origin main

# Use conventional commit format
# feat: New feature
# fix: Bug fix
# test: Adding tests
# refactor: Code refactoring
# docs: Documentation updates
```

**Pre-commit Checklist** (ALL must pass):
- [ ] All tests pass (`uv run pytest -v`)
- [ ] Type checking passes (`uv run pyright moneyflow/`)
- [ ] Code formatting passes (`uv run ruff format --check moneyflow/ tests/`)
- [ ] Linting passes (`uv run ruff check moneyflow/ tests/`)
- [ ] Markdown formatting passes (if docs changed):
  - `markdownlint --config .markdownlint.json README.md 'docs/**/*.md'`
  - `.github/scripts/check-arrow-lists.sh`
- [ ] Coverage hasn't decreased
- [ ] No debug print statements left in code
- [ ] Updated tests for any changed behavior
- [ ] Ran with real test data if changing API logic

### Static Type Checking (NEW)

**Pyright** is integrated for static type analysis. Use comprehensive type hints for all new code.

```bash
# Type-check specific module
uv run pyright moneyflow/data/data_manager.py

# Type-check all application code
uv run pyright moneyflow/

# Type checking is also run in CI on every push
```

**Type Hint Requirements**:
- All function signatures must have full type hints
- Use `TypedDict` for complex dictionaries
- Use `Literal` types for string enums
- Use `NamedTuple` for data transfer objects
- Prefer `Callable[[Args], Return]` for function types

## Performance Considerations

- Bulk fetch transactions on startup (1000 per batch)
- All aggregations done locally with Polars
- Batch API updates to minimize round trips
- Cache data in AppState to avoid re-fetching

## Known Issues / TODOs

- [ ] Add transaction deletion with confirmation
- [ ] Implement time range picker UI
- [ ] Add CSV export functionality
- [ ] Improve duplicate detection algorithm
- [ ] Add split transaction support
- [ ] Implement transaction notes editing
