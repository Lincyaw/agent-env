# Quick Publishing Guide

## One-Time Setup

```bash
# Get your PyPI token from https://pypi.org/manage/account/token/
export UV_PUBLISH_TOKEN=pypi-YOUR-TOKEN

# Add to your shell profile for persistence
echo 'export UV_PUBLISH_TOKEN=pypi-YOUR-TOKEN' >> ~/.bashrc
```

## Publish New Version

```bash
# 1. Update version in sdk/python/arl/pyproject.toml
# 2. Run:
make publish
```

That's it! The `make publish` command will:
1. Generate CRDs
2. Generate Python SDK from OpenAPI
3. Build the package
4. Publish to PyPI

## Test Before Publishing

```bash
# Use Test PyPI first
make publish-test

# Then install and test
pip install --index-url https://test.pypi.org/simple/ \
    --extra-index-url https://pypi.org/simple/ arl
```

## Available Commands

- `make build-sdk` - Just build (no publish)
- `make publish-test` - Publish to Test PyPI
- `make publish` - Publish to Production PyPI
- `make clean-sdk` - Clean build artifacts

See [PUBLISHING.md](PUBLISHING.md) for detailed guide.
