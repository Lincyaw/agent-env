# AI Coding Agent Instructions

## Language & Code Style

- **Code files**: English only for all code, comments, and variable names
- **Documentation**: Can use Chinese in markdown files if appropriate
- **Do not create**: Test files unless explicitly requested or absolutely necessary
- **Format before commit**: Run `make fmt`, `make vet`, `make tidy`
- **Go version**: Using Go 1.25.0 - follow latest best practices
- **DO NOT write** documentation unless specifically asked


## Python

- use `uv run python` to run Python scripts
- use `uv add <package>` to install Python packages
- Use Pydantic models; never pass raw dictionaries for business data
- Error handling: Raise exceptions instead of returning error codes
- **Type hints**: Use modern syntax (`dict`, `list`, `tuple`, `set`) instead of deprecated `typing.Dict`, `typing.List`, etc. Use `|` for unions instead of `Union[]`
  - ✅ `def func(data: dict[str, int]) -> list[str] | None:`
  - ❌ `def func(data: Dict[str, int]) -> Optional[List[str]]:`
- **Python package management**: Use `uv` exclusively (not pip/poetry/conda)
- **Code quality checks are mandatory before committing**: `make check`
- DO NOT write .md documents unless user specifically requests it
- Use type hints extensively; avoid `Any` type
- This is a new project; refactor code aggressively to maintain high quality, no backward compatibility needed
- Do not add comments everywhere, only where necessary for clarity/design rationale