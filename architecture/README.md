# Architecture Index

This directory contains structured YAML files documenting the ARL-Infra architecture, dependencies, and change propagation rules. These files are designed to be:

- **Human-readable**: Clear, well-commented YAML format
- **LLM-parseable**: Structured format for automated analysis
- **Maintainable**: Easy to update as the architecture evolves

## Files

| File | Description |
|------|-------------|
| `components.yaml` | Component catalog with responsibilities, interfaces, and configuration |
| `dependencies.yaml` | Technical and logical dependency graph between components |
| `propagation-rules.yaml` | Change impact analysis rules and required actions |

## Usage

### Validate Architecture Files

```bash
make arch-check
```

This validates:
- YAML syntax correctness
- File path existence
- Dependency consistency

### Query Dependencies

```bash
# Show dependencies of a component
uv run python hack/arch-lint.py query --component sandbox-controller

# Show what depends on a component
uv run python hack/arch-lint.py query --component sandbox-crd --reverse
```

## Maintenance Guidelines

1. **When adding a new component**: Add entry to `components.yaml` with all required fields
2. **When changing dependencies**: Update `dependencies.yaml` and related propagation rules
3. **When modifying CRDs or Proto**: Review `propagation-rules.yaml` for required downstream actions

## Schema Reference

### Component Fields

- `name`: Unique identifier
- `type`: One of `crd`, `controller`, `sidecar`, `proto`, `sdk`, `operator`, `webhook`, `config`
- `paths`: List of file paths (relative to repo root)
- `responsibilities`: List of what this component does
- `interfaces`: Map of interface types (`inputs`, `outputs`, `watches`, `exposes`)
- `config`: Optional configuration details

### Dependency Types

- `import`: Go package import
- `watch`: Kubernetes watch on resource type
- `grpc`: gRPC client-server communication
- `references`: Logical reference (e.g., CRD field to CRD)
- `generates`: Code generation output
- `implements`: Implements an interface definition
- `includes`: Includes/bundles other resources
- `deploys`: Deploys/installs a component
