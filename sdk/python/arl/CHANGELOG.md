# CHANGELOG

All notable changes to the ARL Python SDK will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2026-02-14

### Added
- `GatewayClient` for communicating with the Gateway REST API
- Workspace snapshot and restore support (`session.restore(snapshot_id)`)
- Execution history retrieval (`session.get_history()`)
- Trajectory export for RL/SFT training (`session.export_trajectory()`)
- Tool invocation support (`session.list_tools()`, `session.call_tool()`)
- `InteractiveShellClient` for WebSocket-based shell sessions
- Structured response types: `ExecuteResponse`, `StepResult`, `StepOutput`, `SessionInfo`
- `ToolsSpec`, `InlineToolSpec`, `ToolsRegistry` types for tool management

### Changed
- SDK now communicates via Gateway HTTP API instead of direct Kubernetes API
- `SandboxSession` takes `gateway_url` parameter (default: `http://localhost:8080`)
- `execute()` returns `ExecuteResponse` with per-step results (not a dict)
- Access results via `result.results[0].output.stdout` (not `result["status"]["stdout"]`)
- `WarmPoolManager` updated for new CRD schema

### Removed
- Auto-generated `arl_client` module (OpenAPI-generated code)
- Direct Kubernetes API dependency for execution
- Callback hooks (`register_callback`)
- `TaskStep` TypedDict (replaced by `StepRequest`)

## [0.1.0] - 2026-01-06

### Added
- Initial release of unified ARL Python SDK
- `SandboxSession` class for high-level sandbox management
- Context manager support for automatic resource cleanup
- `TaskStep` TypedDict for type-safe task definitions
- Support for Command and FilePatch task types
- Auto-generated OpenAPI client (internal `_client` module)
- Full type hints with mypy strict mode
- Comprehensive examples in `examples/python/`
- Documentation in `sdk/python/arl/README.md`

### Features
- Create sandboxes from warm pools
- Execute multi-step task pipelines
- Environment variable and working directory support
- Automatic sandbox cleanup
- Sandbox reuse for multiple tasks
- Timeout configuration
- Kubernetes integration via official client

### Developer Experience
- Modern PEP 621 pyproject.toml format
- uv-based package management
- Ruff for linting
- MyPy for type checking
- Hatchling build backend

[0.2.0]: https://github.com/Lincyaw/agent-env/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/Lincyaw/agent-env/releases/tag/v0.1.0
