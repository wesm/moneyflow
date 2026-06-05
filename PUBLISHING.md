# Publishing moneyflow to PyPI

## Prerequisites

1. **PyPI Account**: Create accounts on [PyPI](https://pypi.org/account/register/) and [TestPyPI](https://test.pypi.org/account/register/)
2. **API Tokens**: Generate API tokens for both (Account Settings → API tokens)
3. **Configure credentials**:

```bash
cat > ~/.pypirc << 'EOF'
[distutils]
index-servers =
    pypi
    testpypi

[pypi]
username = __token__
password = pypi-YOUR_PYPI_TOKEN_HERE

[testpypi]
repository = https://test.pypi.org/legacy/
username = __token__
password = pypi-YOUR_TESTPYPI_TOKEN_HERE
EOF

chmod 600 ~/.pypirc
```

---

## Publishing Workflow

Use the single release entrypoint for normal releases:

```bash
./scripts/release.sh 0.2.0
```

This script:

- Generates and previews a deterministic changelog from commit subjects
- Runs the version bump checks
- Updates `pyproject.toml`, `mkdocs.yml`, and `uv.lock`
- Commits the version bump and creates a release tag
- Recreates the tag as an annotated tag with the accepted changelog
- Tests the built package locally
- Pushes the release commit and tag
- Prompts for TestPyPI and production PyPI publishing

Dry-run the planned flow first:

```bash
./scripts/release.sh 0.2.0 --dry-run
```

Useful options:

```bash
./scripts/release.sh 0.2.0 --skip-testpypi
./scripts/release.sh 0.2.0 --skip-pypi
./scripts/release.sh 0.2.0 --skip-push
```

### Test from TestPyPI

If you publish to TestPyPI, verify the package before publishing to production:

```bash
uvx --index-url https://test.pypi.org/simple/ --extra-index-url https://pypi.org/simple/ moneyflow --demo
```

### Post-Publish Automation

Post-publish automation updates the `stable` branch and pushes it to trigger
docs deployment. It changes branches, so it is opt-in:

```bash
./scripts/release.sh 0.2.0 --post-publish
```

Post-publish automation requires a successful production PyPI publish in the
same `release.sh` run. To run it after manually publishing, pass the explicit
override:

```bash
./scripts/release.sh 0.2.0 --post-publish --force-post-publish
```

Or run the subscript after publishing:

```bash
./scripts/post-publish.sh v0.2.0
```

---

## Quick Reference

```bash
# Full release workflow
./scripts/release.sh 0.2.0
```

See `scripts/README.md` for detailed script documentation.

---

## Troubleshooting

### Script permission denied

```bash
chmod +x scripts/*.sh
```

### "Filename already used" on PyPI

You can't re-upload the same version. Bump version and try again.

TestPyPI allows re-uploads for testing.

### uvx can't find command after install

Check entry point in pyproject.toml:

```toml
[project.scripts]
moneyflow = "moneyflow.app:main"
```

---

## After Publishing

Your package will be available at:

- **PyPI**: <https://pypi.org/project/moneyflow/>
- **Install**: `pip install moneyflow`
- **Run**: `uvx moneyflow`
