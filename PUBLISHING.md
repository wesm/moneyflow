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

Use the automated scripts in `scripts/` directory:

### 1. Bump Version

```bash
./scripts/bump-version.sh 0.2.0
```

This updates pyproject.toml, commits, and creates a git tag.

### 2. Test Build Locally

```bash
./scripts/test-build.sh
```

Builds and tests the package with uvx before uploading.

### 3. Publish to TestPyPI

```bash
./scripts/publish-testpypi.sh
```

Runs tests, builds, and uploads to TestPyPI.

### 4. Test from TestPyPI

```bash
uvx --index-url https://test.pypi.org/simple/ --extra-index-url https://pypi.org/simple/ moneyflow --demo
```

### 5. Publish to PyPI

```bash
./scripts/publish-pypi.sh
```

Publishes to production PyPI (with confirmation prompts).

### 6. Push to GitHub

```bash
git push && git push --tags
```

### 7. Post-Publish Automation

```bash
./scripts/post-publish.sh v0.2.0
```

This automates:
- Generating latest screenshots
- Committing to moneyflow-assets with squashed history
- Updating stable branch to release tag

---

## Quick Reference

```bash
# Full release workflow
./scripts/bump-version.sh 0.2.0
./scripts/test-build.sh
./scripts/publish-testpypi.sh
# Test from TestPyPI...
./scripts/publish-pypi.sh
git push && git push --tags
./scripts/post-publish.sh v0.2.0
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

- **PyPI**: https://pypi.org/project/moneyflow/
- **Install**: `pip install moneyflow`
- **Run**: `uvx moneyflow`
