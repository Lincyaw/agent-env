# agent-env

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.26+-00ADD8.svg)](https://golang.org/)
[![Python Version](https://img.shields.io/badge/python-3.10+-3776AB.svg)](https://python.org/)

Gateway and Python SDK for agent-sandbox-backed Agentic Reinforcement Learning
environments with warm pools and sidecar execution.

## Python SDK

```bash
pip install arl-env
```

```python
from arl import SandboxSession

with SandboxSession(
    image="python:3.12",
    profile="my-python-pool",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        {"name": "hello", "command": ["echo", "Hello, World!"]},
    ])
    print(result.results[0].output.stdout)
```

## Local Development

```bash
git clone https://github.com/Lincyaw/agent-env.git
cd agent-env

devbox shell
make generate
make check
make build-sdk
```

## Deploy

```bash
helm dependency build charts/agent-env
helm upgrade --install agent-env charts/agent-env -n arl --create-namespace
```

Set production auth, registry, and image tag values explicitly for real
clusters.

## Agent Plugin

Install the ARL agent plugin to give Claude Code or Codex ARL-specific skills.

Claude Code:

```text
/plugin marketplace add Lincyaw/agent-env
/plugin install arl@arl
/reload-plugins
```

Codex:

```bash
curl -fsSL https://raw.githubusercontent.com/Lincyaw/agent-env/main/plugin/install.sh | bash
```

See `plugin/README.md` for tag-pinned installs, local development commands, and
the list of included skills.

## License

Apache-2.0. See [LICENSE](LICENSE).
