# AI Coding Agent Instructions

## Language & Code Style

- **Code files**: English only for all code, comments, and variable names
- **Documentation**: Can use Chinese in markdown files if appropriate
- **Do not create**: Test files unless explicitly requested or absolutely necessary
- **Format before commit**: Run `make fmt`, `make vet`, `make tidy`
- **Go version**: Using Go 1.25.0 - follow latest best practices
- **DO NOT write** documentation unless specifically asked

## Architecture Change Management

**CRITICAL**: After any code change, ALWAYS perform impact analysis using architecture files:

1. **Query Impact Range**: Check `architecture/propagation-rules.yaml` to identify:
   - Which components are affected by your changes
   - What actions are required (automated commands or manual reviews)
   - Which files need updates due to dependencies in `architecture/dependencies.yaml`

2. **Execute Required Actions**: Run all commands specified in matching propagation rules:
   - Automated: Run `make` commands (e.g., `make manifests`, `make proto-go`, `make sdk-python`)
   - Manual reviews: Check and update files listed in the rule's `manual` actions

3. **Maintain Architecture Files**: When making significant changes, update architecture documentation:
   - **Add new components**: Update `architecture/components.yaml` with component details
   - **Change dependencies**: Update `architecture/dependencies.yaml` with new/modified relationships
   - **New propagation patterns**: Update `architecture/propagation-rules.yaml` with impact rules
   - **Validate changes**: Run `make arch-check` to verify architecture files are consistent

4. **Scope of "Significant Changes"**:
   - Adding/removing components (controllers, CRDs, webhooks, services)
   - Changing component interfaces (API fields, proto methods, gRPC endpoints)
   - Adding/removing dependencies between components
   - Modifying responsibility boundaries

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