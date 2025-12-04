# moneyflow - Codex Development Guide

This file mirrors the expectations in `CLAUDE.md`, phrased for Codex/ChatGPT agents.

## Git & Branch Safety
- Never change or create git branches without explicit user permission.
- Stay on the current branch; ask before any `git checkout` or branch creation.

## Tooling Rules (uv required)
- Use `uv run <command>` for all project commands (tests, lint, app). Do not use pip/poetry/pipenv.
- Do not install or upgrade tools in the user environment. Dependencies must live in `pyproject.toml`.
- Always prefix project commands with `uv run` (e.g., `uv run pytest`, `uv run moneyflow`).

## Code Locations
- All Python code belongs in `moneyflow/`; tests belong in `tests/`. No top-level `.py` files.

## Docstrings
- Use numpydoc format for new/updated docstrings across the codebase.

## Required Checks (before any commit)
- `uv run pytest -v`
- `uv run pyright moneyflow/`
- `uv run ruff format --check moneyflow/ tests/`
- `uv run ruff check moneyflow/ tests/`
- Run markdown checks if docs change: `markdownlint --config .markdownlint.json README.md 'docs/**/*.md'` and `.github/scripts/check-arrow-lists.sh`

## Style Expectations
- Prefer type hints everywhere; keep imports at the top (avoid inline imports unless necessary for cycles).
- Keep functions focused and well-named; add brief comments only where logic is non-obvious.
- Respect line length (100) and existing formatting conventions.

## Testing Philosophy
- Follow TDD when adding behavior: write failing tests, then implement, then rerun tests.
- Maintain or improve coverage (core modules target >90%; some are 100% and must stay that way).

## Security & Data
- Never commit real credentials or user data. Do not touch `~/.moneyflow/` or `.mm/` directories.
- Clear or reset backend auth state via provided methods instead of manual file edits.

## Commit Etiquette
- Use conventional commit messages (feat/fix/test/refactor/docs/etc.) after all checks pass.
- Never push without explicit user approval.
