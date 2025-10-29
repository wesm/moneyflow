# Documentation Deployment

## Overview

Documentation is deployed to [moneyflow.dev](https://moneyflow.dev) from the **`stable` branch only**.

This ensures that published documentation matches released versions and doesn't expose unreleased features.

---

## Branch Strategy

**`main` branch:**
- Active development
- New features and docs
- Not deployed to moneyflow.dev

**`stable` branch:**
- Points to latest release
- Only released features
- Automatically deployed to moneyflow.dev

---

## Release Workflow

When releasing a new version:

### 1. Tag and Release

```bash
# On main branch
./scripts/bump-version.sh 0.5.4
git push && git push --tags
```

### 2. Update Stable Branch

```bash
# Fast-forward stable to the new release tag
git checkout stable
git merge --ff-only v0.5.4

# Or reset to the tag
git reset --hard v0.5.4

# Push to trigger docs deployment
git push origin stable
```

### 3. Verify Deployment

- GitHub Actions runs the "Deploy Documentation" workflow
- Docs are built from the `stable` branch
- Site updates at https://moneyflow.dev within ~2 minutes

---

## Current State

- **`stable` branch** → Points to `v0.5.3` (commit 5cf55bc)
- **Workflow trigger** → Push to `stable` branch
- **Deployment target** → GitHub Pages (moneyflow.dev)

---

## Manual Deployment

If you need to manually trigger docs deployment:

```bash
# Option 1: Push to stable branch
git checkout stable
git push origin stable

# Option 2: Use workflow_dispatch
# Go to: https://github.com/wesm/moneyflow/actions/workflows/docs.yml
# Click "Run workflow" → Select "stable" branch
```

---

## Troubleshooting

**Docs not updating after release:**
- Check stable branch points to the release tag: `git log stable | head -3`
- Verify workflow ran: GitHub Actions → Deploy Documentation
- Check for workflow errors in GitHub Actions logs

**Wrong version showing on moneyflow.dev:**
- stable branch may not have been updated
- Run: `git checkout stable && git reset --hard v0.5.X && git push --force origin stable`

**Need to update docs without a release:**

This should be rare, but if absolutely necessary (e.g., fix critical doc typo):

```bash
# Make doc fix on main
git checkout main
# ... make changes to docs/ ...
git commit -m "docs: Fix critical typo"

# Cherry-pick to stable
git checkout stable
git cherry-pick <commit-sha>
git push origin stable
```

---

## Files Involved

- `.github/workflows/docs.yml` - Deployment workflow
- `mkdocs.yml` - MkDocs configuration
- `docs/` - Documentation source files
- `stable` branch - Release branch for docs deployment
