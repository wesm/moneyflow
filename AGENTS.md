# AGENTS.md - Working With Codex + Claude Code

This repo uses **uv** for all tooling, and has strict safety and quality rules.
Follow this file plus `CLAUDE.md` (do not edit it).

## Branch Safety (Critical)
- Never switch branches or create new branches without explicit user permission.
- Never run `git checkout` unless the user asks you to.
- Never amend commits unless explicitly asked.

## Environment & Tooling (uv only)
- Use `uv run <command>` for all tooling; do not use pip/poetry/pipenv.
- If modules are missing, run `uv sync` (do not `pip install`).

Common commands:
```bash
uv run pytest -v
uv run pytest --cov --cov-report=term-missing
uv run pyright
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
```

## Type Checking
- Run `uv run pyright` (pyright is in the uv environment).
- Keep functions fully typed; avoid inline imports unless required for circular deps.

## Testing & Coverage
- TDD is required for new features/bug fixes.
- Coverage should not decrease; business logic targets >90%.
- Full suite before commits: tests, pyright, ruff format/check.

## Project Structure
- All Python code lives under `moneyflow/`.
- Tests go in `tests/`.
- Avoid top-level `.py` files.

## Cache Consistency Notes (Recent Work)
- Cache orchestration is extracted to `moneyflow/cache_orchestrator.py`.
- App integration lives in `moneyflow/app.py` (uses `CacheOrchestrator`).
- Core cache logic in `moneyflow/cache_manager.py`.
- Tests for orchestration: `tests/test_cache_orchestrator.py`.
- Tests for cache manager: `tests/test_cache.py`.

## Edits & Formatting
- Prefer small, targeted changes.
- Use ASCII by default; avoid adding Unicode unless file already uses it.
- Keep comments focused on “why”, not “what”.

## Before Committing (Must Pass)
```bash
uv run pytest -v
uv run pyright
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
```
If docs changed:
```bash
markdownlint --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```
