# Python Code Quality Standards

## Linting & Formatting Tools

### Ruff (Recommended)
Fast, comprehensive linter and formatter that replaces multiple tools.

```bash
# Install
uv add --dev ruff

# Run linter
uv run ruff check .

# Auto-fix issues
uv run ruff check --fix .

# Format code
uv run ruff format .
```

Configuration in `pyproject.toml`:
```toml
[tool.ruff]
line-length = 100
target-version = "py312"

[tool.ruff.lint]
select = [
    "E",    # pycodestyle errors
    "W",    # pycodestyle warnings
    "F",    # pyflakes
    "I",    # isort
    "N",    # pep8-naming
    "UP",   # pyupgrade
    "B",    # flake8-bugbear
    "A",    # flake8-builtins
    "C4",   # flake8-comprehensions
    "SIM",  # flake8-simplify
    "TCH",  # flake8-type-checking
]
ignore = ["E501"]  # Line too long (handled by formatter)

[tool.ruff.lint.isort]
known-first-party = ["your_package"]
```

### MyPy
Static type checker for Python.

```bash
# Install
uv add --dev mypy

# Run type checking
uv run mypy .
```

Configuration in `pyproject.toml`:
```toml
[tool.mypy]
python_version = "3.12"
warn_return_any = true
warn_unused_configs = true
disallow_untyped_defs = true
disallow_incomplete_defs = true
check_untyped_defs = true
no_implicit_optional = true
warn_redundant_casts = true
warn_unused_ignores = true
strict_equality = true
```

## Best Practices

### Type Hints
Always use type hints for function signatures and class attributes.

```python
from typing import Optional, Union
from collections.abc import Sequence

def process_items(
    items: Sequence[str],
    max_count: int = 10,
    callback: Optional[callable] = None,
) -> list[dict[str, str]]:
    """Process items and return results."""
    ...
```

### Modern Python Patterns

**Use match statements (Python 3.10+):**
```python
match response.status:
    case 200:
        return response.json()
    case 404:
        raise NotFoundError()
    case _:
        raise APIError(f"Unexpected status: {response.status}")
```

**Use structural pattern matching:**
```python
match point:
    case (0, 0):
        return "Origin"
    case (x, 0):
        return f"X-axis at {x}"
    case (0, y):
        return f"Y-axis at {y}"
    case (x, y):
        return f"Point at ({x}, {y})"
```

**Use walrus operator when appropriate:**
```python
# Good
if (match := pattern.search(text)):
    process(match.group(1))

# Instead of
match = pattern.search(text)
if match:
    process(match.group(1))
```

### Error Handling

**Use specific exceptions:**
```python
class ConfigurationError(Exception):
    """Raised when configuration is invalid."""
    pass

def load_config(path: str) -> dict:
    if not Path(path).exists():
        raise ConfigurationError(f"Config file not found: {path}")
    ...
```

**Context managers for resource management:**
```python
from contextlib import contextmanager

@contextmanager
def managed_resource():
    resource = acquire_resource()
    try:
        yield resource
    finally:
        resource.cleanup()
```

### Documentation

**Use comprehensive docstrings (Google style):**
```python
def calculate_metrics(
    data: pd.DataFrame,
    window_size: int = 30,
    normalize: bool = True,
) -> dict[str, float]:
    """Calculate rolling window metrics for the dataset.
    
    Args:
        data: Input DataFrame with time series data
        window_size: Number of periods for rolling calculation
        normalize: Whether to normalize values to [0, 1]
    
    Returns:
        Dictionary containing metric names and calculated values
    
    Raises:
        ValueError: If window_size is larger than data length
        KeyError: If required columns are missing from data
    
    Examples:
        >>> df = pd.DataFrame({'value': [1, 2, 3, 4, 5]})
        >>> metrics = calculate_metrics(df, window_size=2)
        >>> metrics['mean']
        3.0
    """
    ...
```

### Testing

**Use pytest with fixtures and parametrize:**
```python
import pytest

@pytest.fixture
def sample_data():
    return {"key": "value", "count": 42}

@pytest.mark.parametrize("input,expected", [
    (1, 2),
    (2, 4),
    (3, 6),
])
def test_double(input, expected):
    assert double(input) == expected
```

### Project Structure

```
project/
├── pyproject.toml          # All configuration
├── README.md
├── src/
│   └── package_name/
│       ├── __init__.py
│       ├── core.py
│       └── utils.py
├── tests/
│   ├── __init__.py
│   ├── test_core.py
│       └── test_utils.py
└── docs/
```

### Dependencies Management

Use `uv` for modern Python package management:

```bash
# Add dependency
uv add requests

# Add dev dependency
uv add --dev pytest ruff mypy

# Run command in environment
uv run python script.py
uv run pytest

# Sync dependencies
uv sync
```
