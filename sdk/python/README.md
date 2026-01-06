# Python SDK Architecture

## Overview

ARL 的 Python SDK 采用**双包架构**，将自动生成的 API 客户端代码与手工维护的高级封装层分离：

```
sdk/python/
├── arl-client/          # 自动生成的 OpenAPI 客户端（不要手动编辑）
│   └── arl_client/      # 低级 API 绑定
└── arl-wrapper/         # 手工维护的高级封装（可以自由修改）
    └── arl/             # SandboxSession 等高级 API
```

## 设计原因

### 问题

OpenAPI Generator 会覆盖所有生成的文件，包括自定义代码。尝试使用 `.openapi-generator-ignore` 来保护文件效果不理想。

### 解决方案

将代码分为两个独立的包：

1. **arl-client** (自动生成)
   - 由 `make sdk-python` 从 CRD schemas 自动生成
   - 提供低级的 Kubernetes CRD 操作 API
   - **永远不要手动编辑此包中的代码**

2. **arl-wrapper** (手工维护)
   - 依赖于 `arl-client`
   - 提供用户友好的高级 API（如 `SandboxSession`）
   - 可以自由添加/修改代码
   - **不会被代码生成过程影响**

## 使用示例

用户只需要导入 `arl`：

```python
from arl import SandboxSession

with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
    result = session.execute([
        {
            "name": "hello",
            "type": "Command",
            "command": ["echo", "Hello!"],
        }
    ])
    print(result["status"]["stdout"])
```

## 代码生成流程

```bash
make sdk-python
```

执行步骤：

1. **提取 CRD schemas** - `merge-openapi.py` 从 `config/crd/*.yaml` 提取 OpenAPI schemas
2. **合并为统一 spec** - 生成 `openapi/arl-api.yaml`
3. **生成 Python 客户端** - OpenAPI Generator 生成 `arl-client/` 中的代码
4. **后处理** - `fix-arl-client-pyproject.py` 修复生成的 `pyproject.toml` 使其兼容现代工具（uv）

## 开发工作流

### 修改 CRD 后重新生成客户端

```bash
# 1. 修改 CRD 定义
vim config/crd/arl.infra.io_tasks.yaml

# 2. 重新生成客户端
make sdk-python

# 3. 同步依赖
uv sync

# 4. 如果 API 变化，更新 wrapper 代码
vim sdk/python/arl-wrapper/arl_wrapper/session.py
```

### 添加新的高级功能

直接在 `arl-wrapper` 中添加：

```bash
# 添加新的辅助函数
vim sdk/python/arl-wrapper/arl/helpers.py

# 更新类型定义
vim sdk/python/arl-wrapper/arl/types.py

# 无需担心代码生成覆盖！
```

## 依赖关系

```
examples/python/*.py
       ↓ imports
arl (arl-wrapper 包，手工维护)
       ↓ depends on
arl-client (自动生成)
       ↓ uses
kubernetes Python client
```

## 文件说明

### 自动生成（不要编辑）

- `sdk/python/arl-client/` - 整个目录
- 生成时会覆盖的文件包括：
  - `arl_client/*.py`
  - `arl_client/api/*.py`
  - `arl_client/models/*.py`
  - `docs/*.md`
  - `test/*.py`

### 手工维护（安全编辑）

- `sdk/python/arl-wrapper/` - 整个目录
  - `arl/session.py` - SandboxSession 类
  - `arl/types.py` - 类型定义
  - `arl/__init__.py` - 包入口
  - `pyproject.toml` - 依赖配置
  - `README.md` - 用户文档

### 生成脚本

- `hack/generate-sdk.sh` - 主生成脚本
- `hack/merge-openapi.py` - CRD schema 提取与合并
- `hack/fix-arl-client-pyproject.py` - 后处理脚本（修复 pyproject.toml）

## 版本管理

- **arl-client**: 版本号由 OpenAPI Generator 配置中的 `packageVersion` 参数控制
- **arl-wrapper**: 版本号在其 `pyproject.toml` 中手动管理

两个包可以独立版本演进，但建议保持同步以避免兼容性问题。

## 优势

1. ✅ **代码隔离** - 自动生成的代码永远不会破坏手工代码
2. ✅ **简化开发** - 开发者可以专注于 wrapper 层的业务逻辑
3. ✅ **易于更新** - CRD 变更后只需重新生成客户端，wrapper 层保持不变（除非 API 有破坏性变更）
4. ✅ **清晰职责** - 客户端负责 API 绑定，wrapper 负责用户体验
5. ✅ **测试友好** - 可以独立测试 wrapper 层的逻辑
