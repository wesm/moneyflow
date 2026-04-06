# Package Reorganization Plan

> **For agentic workers:** Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reorganize moneyflow into three packages (`data/`, `backends/`, `tui/`) with only entry-point modules at root.

**Architecture:** Move files via `git mv`, then bulk-rewrite all imports (both absolute `from moneyflow.X` and relative `from .X`). Inline `filter_service.py` into `state.py`. Update pyproject.toml entry points.

**Tech Stack:** git mv, sed/Python for bulk import rewriting, pytest for verification.

---

### Target Layout

```
moneyflow/
  __init__.py, cli.py, version.py, logging_config.py, retry_logic.py, auditor.py
  data/          state, data_manager, commit_orchestrator, duplicate_detector,
                 categories, cache_manager, cache_orchestrator, credentials,
                 account_manager, migration, amazon_linker, demo_data_generator,
                 time_navigator
  backends/      base, demo, amazon, ynab, ynab_client, monarch (was monarchmoney),
                 monarch_queries (was queries), gql_version
  tui/           app, app_controller, account_flow, amazon_presentation,
                 backend_task_runner, textual_view, view_presenter, view_interface,
                 formatters, modal_helper, keybindings, theme_manager,
                 notification_helper, screens/, widgets/, styles/
```

### Task 1: Create directories and move files

- [ ] Create `moneyflow/data/`, `moneyflow/tui/` with `__init__.py`
- [ ] `git mv` all files to their new locations (including screens/, widgets/, styles/ into tui/)
- [ ] Rename: `monarchmoney.py` -> `backends/monarch.py`, `queries.py` -> `backends/monarch_queries.py`
- [ ] Move `gql_version.py` and `ynab_client.py` into `backends/`
- [ ] Delete `filter_service.py` (inline its content into `state.py`)

### Task 2: Rewrite all imports

Rewrite every import across moneyflow/ and tests/ using this mapping:

**To `moneyflow.data.*`:** state, data_manager, commit_orchestrator, duplicate_detector, categories, cache_manager, cache_orchestrator, credentials, account_manager, migration, amazon_linker, demo_data_generator, time_navigator

**To `moneyflow.tui.*`:** app, app_controller, account_flow, amazon_presentation, backend_task_runner, textual_view, view_presenter, view_interface, formatters, modal_helper, keybindings, theme_manager, notification_helper

**To `moneyflow.backends.*`:** monarchmoney -> backends.monarch, queries -> backends.monarch_queries, gql_version -> backends.gql_version, ynab_client -> backends.ynab_client

**Relative imports** within each package must be updated (e.g., files in `tui/` that import other `tui/` files use `from .X`, files that import across packages use `from ..data.X`).

### Task 3: Update entry points and config

- [ ] Update pyproject.toml `moneyflow-setup` entry point
- [ ] Update `moneyflow/__init__.py` re-exports
- [ ] Update any scripts/ references

### Task 4: Verify

- [ ] `uv run pytest -x -q` (all 1405 tests pass)
- [ ] `uv run ruff check moneyflow/ tests/`
- [ ] `uv run ruff format --check moneyflow/ tests/`
- [ ] `uv run pyright moneyflow/`
- [ ] Commit
