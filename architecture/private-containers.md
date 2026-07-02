# Gateway-Managed Private Containers

## Status

MVP implemented for gateway-managed private containers, Kubernetes
container-targeted exec, and Python SDK declaration/execution support.
Benchmark adapters and artifact collection remain future work.

## Summary

ARL should let a session declare additional gateway-managed containers that
are present in the sandbox Pod but are not part of the agent-facing executor
environment. The gateway can execute commands in those containers on demand.
This gives evaluators a general way to carry private tests, checkers, fixtures,
golden files, or large datasets without uploading them through the gateway and
without making those files visible to the main agent container.

The important abstraction is not "copy tests into the sandbox". It is "create
private containers that the gateway can operate, while the agent can only
operate the executor container". Copying files is one possible command run in a
private container, but evaluation should usually run the tests inside the
private container instead of copying private tests into the executor when the
primary requirement is "the agent must not be able to read private tests".

## Problem

The current Terminal-Bench style evaluator uploads tests after replaying an
agent trajectory:

1. Run the agent in an executor image that does not include tests.
2. Create an eval session from the same executor image.
3. Replay the agent trajectory in the eval executor.
4. Upload tests through `PUT /v1/sessions/{id}/files/...`.
5. Run `/tests/run-tests.sh` in the executor.

This has two problems:

- Large test trees are slow and fragile to upload. Recent runs hit intermittent
  `gRPC WriteFile send failed: EOF` errors while uploading multi-megabyte test
  payloads.
- The design is benchmark-specific. It treats "tests are files to upload" as a
  platform behavior, but future evaluators may need scripts, generated cases,
  checkers, model artifacts, compressed datasets, or golden outputs.

There is also a security boundary to preserve: private tests must not be
available to the model during the agent-solving phase, or during eval replay.
Otherwise an agent can inspect the tests and write a targeted solution.

## Goals

- Support eval-only assets without remote file uploads.
- Keep private assets invisible from the executor container during agent
  execution and eval replay.
- Let evaluators run arbitrary setup or evaluation scripts from private images.
- Let private evaluation commands run with the same workspace path and working
  directory expected by benchmark scripts.
- Keep the existing executor execution path unchanged for normal sessions.
- Make the API general enough for private tests, checkers, fixtures, datasets,
  and generated eval cases.
- Preserve Kubernetes warm-pool compatibility: private containers are part of
  the SandboxTemplate identity and are fixed when the Pod is created.

## Design Targets

The design should support two separate targets. They are related, but they are
not the same requirement.

### Target A: Private Test Confidentiality

The executor must not be able to read private test scripts, private test data,
checkers, fixtures, or golden files. This is the target needed to prevent the
model from inspecting hidden tests and writing a test-specific solution.

The recommended solution is to keep private assets in a private container image
and run the evaluator inside that private container. The private container
mounts the shared workspace at the same path as the executor, so benchmark
scripts can still run from the expected project directory:

```bash
cd /app/project
TEST_DIR=/tests bash /tests/run-tests.sh
```

The executor can read `/app/project`, but it cannot read `/tests` because
`/tests` exists only in the private container image filesystem.

### Target B: Executor-Fidelity Evaluation

Some benchmarks require the final test command to run inside the executor
container. Reasons include:

- the agent may install packages or mutate global paths outside the workspace;
- the task may start services that tests expect to reach through localhost;
- the test script may depend on executor-only users, permissions, processes, or
  runtime state;
- the benchmark contract explicitly says tests run in the same container that
  the agent modified.

The compatible solution is materialization: a private container prepares or
copies eval assets into an executor-visible path after the agent finishes, then
the gateway runs the final test command in the executor.

This preserves executor fidelity, but it weakens private-test confidentiality
after materialization. It should be an explicit eval mode, not the default for
hidden-test benchmarks.

## Evaluation Modes

| Mode | Where tests live | Where test command runs | Agent can read tests? | Execution fidelity | Typical use |
| --- | --- | --- | --- | --- | --- |
| `private_container` | Private image filesystem | Private container | No | Good for workspace-based tasks | Hidden tests, checkers, fixtures |
| `single_session_private_container` | Private image filesystem | Private container after agent run | No, unless the private container exposes them | Good for workspace-based tasks; agent runtime remains live | Fast interactive eval without replay |
| `replay_private_container` | Private image filesystem | Private container after replay | No | Good for trajectory-based benchmark scoring | Batch eval, pass@k, reproducible scoring |
| `materialized_executor` | Copied to shared workspace or `/tests` | Executor | Yes after materialization | Highest executor fidelity | Tests must run in executor |
| `upload_executor` | Uploaded to shared workspace or `/tests` | Executor | Yes after upload | Existing behavior | Backward compatibility |

`private_container`, `single_session_private_container`, and
`replay_private_container` all satisfy Target A if private assets are not
served over localhost and are not copied into shared volumes. `materialized`
and `upload` modes satisfy Target B but should not be described as hidden-test
secure after the copy/upload point.

## Non-Goals

- Dynamically mounting an image into a running executor container after replay.
  Kubernetes does not support adding container mounts to an existing Pod.
- Hiding the existence of localhost ports or Pod networking from the executor.
  The requirement is to hide private files and asset image contents, not to hide
  the ARL control plane.
- Guaranteeing secrecy after private data is intentionally copied into a
  shared executor-visible path. That mode is useful, but it is weaker than
  running evaluation inside a private container.
- Replacing the sidecar or executor-agent execution path for the main executor.

## Design Principles

1. The executor is the only agent-facing container.
2. Private containers are gateway-facing containers.
3. Private asset files are never mounted into the executor.
4. The gateway decides when private commands run.
5. The safest evaluator runs inside the private container and reads the shared
   workspace, instead of copying private tests into the executor.
6. Materializing private files into executor-visible storage is explicit and
   opt-in.
7. The workspace mount path is configurable so private commands can run from
   the same project directory that benchmark scripts expect.

## Pod Model

A sandbox Pod may contain:

- `executor`: the task image. Agent commands run here. It mounts the shared
  workspace volume and the ARL executor-agent socket.
- `sidecar`: ARL control plane. It does not mount the workspace today and does
  not need private asset files.
- `private containers`: evaluator-defined images. These containers may mount
  the shared workspace volume, but their image filesystem is not visible to the
  executor. They do not receive the ARL socket or gRPC token.

Example topology:

```text
Pod
├── executor
│   ├── image: longcli-task-cs61:v0
│   └── mounts: workspace -> /app, arl-socket -> /var/run/arl
├── sidecar
│   └── mounts: arl-socket -> /var/run/arl
└── private container: eval
    ├── image: longcli-task-cs61-eval:v0
    └── mounts: workspace -> /app
```

The private image can contain `/tests`, `/checker`, `/fixtures`, or an
`/eval/run.sh` entrypoint. The executor cannot read those paths because they
belong to the private container image filesystem.

## API Shape

### Session Creation

`POST /v1/sessions` and `POST /v1/managed/sessions` accept optional private
container specs:

```json
{
  "image": "pair/longcli-cs61:v0",
  "experimentId": "eval-longcli-cs61-a0",
  "workspaceDir": "/app",
  "privateContainers": [
    {
      "name": "eval",
      "image": "pair/longcli-cs61-eval:v0",
      "mountWorkspace": true,
      "workspaceMountPath": "/app",
      "command": ["sleep", "infinity"],
      "env": {
        "TEST_DIR": "/tests"
      }
    }
  ]
}
```

Fields:

- `name`: stable container name, DNS label compatible.
- `image`: container image containing private assets and scripts.
- `mountWorkspace`: whether to mount the session workspace volume.
- `workspaceMountPath`: where to mount the workspace in this private container.
  Defaults to the session `workspaceDir`.
- `workspaceAccess`: optional access mode for the workspace mount. Suggested
  values are `readWrite` and `readOnly`. The MVP may support only `readWrite`.
- `command` / `args`: optional long-running container command. Defaults should
  keep the container alive for later gateway exec.
- `env`: static environment variables for the private container.
- `resources`: optional container-specific resource requirements.
- `imagePullPolicy`: optional pull policy, defaulting to the gateway injected
  pull policy.

For managed sessions, the private container spec is part of the managed pool
identity. Sessions with different private container specs must not reuse the
same warm pool.

For direct session creation, `privateContainers` should be used with an
image-backed request so the gateway can create/select a matching managed pool,
or with an explicit `poolName` when the caller has already created a pool with
the same private containers. Profile-only selection is intentionally rejected
for this feature because the gateway cannot otherwise prove that the selected
warm pool contains the requested private containers.

### Container-Targeted Execution

Existing execution remains:

```text
POST /v1/sessions/{id}/execute
```

It targets `executor` through sidecar/executor-agent, as it does today.

New endpoint:

```text
POST /v1/sessions/{id}/containers/{container}/execute
```

Request body uses the same step shape as `ExecuteRequest`:

```json
{
  "steps": [
    {
      "name": "eval",
      "command": ["bash", "-lc", "cd /app && TEST_DIR=/tests bash /tests/run-tests.sh"],
      "workDir": "/app",
      "timeoutSeconds": 1200
    }
  ]
}
```

Response should match `ExecuteResponse` where possible:

```json
{
  "sessionID": "gw-...",
  "container": "eval",
  "results": [
    {
      "index": 0,
      "name": "eval",
      "output": {
        "stdout": "...",
        "stderr": "",
        "exit_code": 0
      },
      "duration_ms": 12345,
      "timestamp": "..."
    }
  ],
  "totalDurationMs": 12345
}
```

Container-targeted execution is an eval/control-plane operation. It should not
be replayed as part of the agent trajectory. It may be written to gateway audit
logs, but it should not become a user action in `ReplayFrom`.

## Execution Backend Options

### Option A: Gateway Kubernetes Exec

The gateway uses the Kubernetes `pods/exec` subresource to run commands in the
target private container.

Pros:

- Minimal Pod changes: no new agent binary or socket protocol in private
  containers.
- Works with arbitrary images that contain a shell or requested command.
- Keeps the existing sidecar focused on executor control.

Cons:

- Requires gateway RBAC for `pods/exec`.
- Streaming and timeout behavior must be implemented carefully.
- Execution path differs from executor execution, so snapshot/history semantics
  should be explicitly separate.
- Kubernetes exec has no native working-directory or environment override; the
  MVP wraps commands with `sh -c` when `workDir` or `env` is set, so private
  eval images that use those fields must include a POSIX-compatible shell.

This is the preferred MVP because it is small and general.

### Option B: Private Container Agent

Each private container includes a small ARL agent or shares a private control
socket with the sidecar.

Pros:

- More uniform execution semantics with the executor path.
- No Kubernetes exec dependency for private commands.

Cons:

- Requires modifying or wrapping private images.
- Adds another in-Pod control protocol.
- Makes the "private asset image" heavier and less benchmark-agnostic.

This can be considered later if Kubernetes exec is not reliable enough.

## Security Model

### What This Protects

- The executor cannot list or read files from private image paths such as
  `/tests`, `/checker`, or `/fixtures`.
- During agent execution and eval replay, no private asset files are mounted in
  the executor.
- The executor does not receive private container control sockets or gateway
  credentials.
- Private containers can be omitted entirely from agent sessions and included
  only in eval sessions.

### What This Does Not Protect

- If a private command writes private files into the shared workspace, the
  executor can read them afterwards.
- If an agent trajectory starts a long-lived background process during replay,
  that process may observe files that are later materialized into
  executor-visible paths.
- If the evaluator writes detailed private test output into the shared
  workspace, the executor can read that output later in the same session.
- If the private container exposes an unauthenticated localhost file service,
  the executor may be able to connect to it through the shared Pod network.

### Recommended Confidentiality Mode

Run the evaluator inside the private container and keep private tests out of
shared volumes:

```text
private container reads:   /tests, /checker, /fixtures
private container reads/writes: shared workspace at /app
executor reads/writes:     shared workspace at /app
executor never sees:       /tests, /checker, /fixtures
```

The private container may write only result artifacts, such as
`/app/test_output/f2p_score.json`, into the shared workspace. It should not
copy private test source files into the workspace unless the benchmark accepts
that weaker boundary.

This mode still supports scripts that must run from a specific project
directory, as long as the required directory is inside the shared workspace.
Mount the workspace at the same path in both containers and set `workDir`:

```json
{
  "steps": [
    {
      "name": "eval",
      "command": ["bash", "-lc", "TEST_DIR=/tests bash /tests/run-tests.sh"],
      "workDir": "/app/project",
      "timeoutSeconds": 1200
    }
  ]
}
```

If a script hardcodes relative paths such as `./run-tests.sh`, the private
image can provide a wrapper that runs from the project directory while keeping
private files outside the workspace:

```bash
cd /app/project
bash /tests/wrapper.sh
```

### Materialization Mode

Some benchmarks may require tests to run inside the executor because the agent
can modify system packages, global runtime state, or services outside the
workspace. For those cases, a private container can copy assets into a shared
workspace staging directory and the executor can run them.

This mode should be explicit:

```json
{
  "name": "materialize-tests",
  "command": ["bash", "-lc", "rm -rf /app/_eval_staging && cp -a /tests /app/_eval_staging"]
}
```

It is less secure than private-container evaluation. It avoids remote upload
costs, but it does not guarantee that a replayed background process cannot
observe the materialized tests.

Use this mode for Target B when executor-fidelity matters more than private
test confidentiality after the agent finishes.

## LongCLI / Terminal-Bench Usage

### Image Build

Build two image families per task:

- Agent image: task environment without private tests.
- Eval private image: usually `FROM` the task image, plus private tests and
  eval scripts.

Example `Dockerfile.eval`:

```dockerfile
FROM pair/longcli-cs61:v0
COPY tests /tests
COPY run-tests.sh /tests/run-tests.sh
RUN chmod +x /tests/run-tests.sh
CMD ["sleep", "infinity"]
```

The eval private image can be pushed as:

```text
pair/longcli-cs61-eval:v0
```

### Eval Flow Options

#### Replay Private-Container Eval

This is the preferred batch-scoring flow for hidden tests:

1. Agent run:
   - Create session with only the agent image.
   - Run the model.
   - Record `session_id`.

2. Evaluation:
   - Create eval session with the same executor image plus private container
     `eval`.
   - Replay the recorded trajectory in the executor.
   - Run the test command in the private `eval` container:

     ```bash
     cd /app && TEST_DIR=/tests bash /tests/run-tests.sh 2>&1
     ```

   - Collect score artifacts from the shared workspace.

This avoids uploading test files and avoids exposing `/tests` to the executor.

#### Single-Session Private-Container Eval

For interactive or fast experiments, the private eval container may be present
in the same session as the agent:

1. Create one session with the executor image plus private container `eval`.
2. Run the model only through executor-targeted execution.
3. After the model finishes, run the test command in the private `eval`
   container.
4. Collect score artifacts from the shared workspace.

This still keeps `/tests` out of the executor as long as the private container
does not expose them over network or copy them into the workspace. It does not
isolate the evaluator from processes or global state left by the agent, but
that is a separate requirement from hiding test files.

#### Materialized Executor Eval

Use this only when tests must run in the executor container:

1. Run the model.
2. Ask the private container to prepare tests into an executor-visible path.
3. Run the final test command through the existing executor endpoint.

This preserves executor runtime fidelity but does not preserve hidden-test
confidentiality after materialization.

### Compatibility Note

If a task's solution depends on changes outside the workspace, private-container
evaluation may not observe those changes unless they are replayed or reproduced
inside the private container. For those tasks, the benchmark can choose
materialization mode or define a private eval image that starts from the same
base and runs setup scripts before testing.

## Implementation Plan

### Phase 1: Platform Primitive

1. Add Go request/response types:
   - `PrivateContainerSpec`
   - `ContainerExecuteRequest`
   - `ContainerExecuteResponse`

2. Extend `CreatePoolRequest`, `CreateSessionRequest`, and
   `CreateManagedSessionRequest` with `privateContainers`.

3. Include private container specs in managed pool identity hashing.

4. Extend `sandboxPodSpec`:
   - Append private containers to `pod.Spec.Containers`.
   - Mount the shared workspace volume only when `mountWorkspace=true`.
   - Do not mount `arl-socket`, `arl-bin`, or gRPC token secrets.
   - Do not expose private container ports by default.

5. Add gateway endpoint:
   - `POST /v1/sessions/{id}/containers/{container}/execute`

6. Implement private container execution with Kubernetes `pods/exec`.

7. Add Python SDK support:
   - `PrivateContainerSpec` model.
   - `GatewayClient.execute_container(...)`.
   - `ManagedSession(..., private_containers=[...])`.

### Phase 2: Benchmark Integration

1. Extend benchmark image build to optionally build eval private images.
2. Add Terminal-Bench adapter support for private-container evaluation.
3. Preserve fallback to current upload behavior when the gateway does not
   support private containers.
4. Record which eval mode was used in score JSON:
   - `eval_mode: "private_container"`
   - `eval_mode: "single_session_private_container"`
   - `eval_mode: "replay_private_container"`
   - `eval_mode: "upload"`
   - `eval_mode: "materialized"`

### Phase 3: Hardening

1. Add per-container network policy controls if private containers should not
   expose localhost services to executor traffic.
2. Add read-only workspace mount support for private checkers that should not
   mutate the workspace.
3. Add configurable output artifact collection.
4. Add timeouts, log streaming, and operation polling for long private eval
   commands.
5. Add metrics for private container exec duration and failures.

## Validation Plan

Unit-level validation:

- Pod template includes private containers with the requested image and no ARL
  socket mounts.
- Executor container does not mount private asset paths.
- Managed pool identity changes when private container specs change.
- Container-targeted execute rejects `executor` if callers should use the
  existing endpoint, and rejects unknown private container names.

Integration validation:

- Create a session with a private container containing `/secret.txt`.
- Verify executor command `test -e /secret.txt` fails.
- Verify private container command `test -e /secret.txt` succeeds.
- Verify private container can write `/workspace/result.txt` and executor can
  read that result.
- Verify private container execution can set `workDir` to a project subdirectory
  under the shared workspace.
- Verify an agent session created without private containers cannot access the
  private image or files.

Benchmark validation:

- Run LongCLI eval for a task with large tests using private-container mode.
- Confirm no `PUT /files` uploads happen for tests.
- Confirm score artifacts match upload-mode results for the same trajectory.

## Open Questions

- Should private containers mount the workspace read-write by default, or
  require an explicit `workspaceAccess: "readWrite"`?
- Should the gateway allow private container execution for user API keys, or
  restrict it to admin/eval credentials?
- Should container-targeted exec be stored in the same history type with a
  `controlPlane=true` marker, or only in audit logs?
- Do we need a standard result artifact convention, such as
  `/workspace/test_output`, across benchmark adapters?
- Should private containers have network disabled by default, or is Kubernetes
  per-container network isolation out of scope for the MVP?
- Should `single_session_private_container` be allowed by default, or should
  benchmark adapters opt into it explicitly to avoid confusing it with replayed
  scoring?
- For tasks that mutate system state outside the workspace, do we accept
  materialization mode or invest in filesystem snapshot transfer that excludes
  live processes?

## Alternatives Considered

### Upload Files After Replay

This is the current behavior. It is simple and preserves executor fidelity, but
large uploads are slow and can fail. It also treats private tests as file
payloads rather than general evaluator assets.

### InitContainer Copies Tests Into Workspace

This avoids upload cost but leaks tests during eval replay. A replayed agent
trajectory can inspect the copied tests before evaluation starts.

### Eval Executor Image Contains Tests

This also leaks tests during eval replay because the executor filesystem
contains private files from the start of the eval session.

### Sidecar Image Contains Tests

The sidecar is global infrastructure, not task-specific evaluator state. It
should remain a minimal static control-plane component. Packing benchmark tests
into the sidecar couples platform release cadence to benchmark assets.

### Copy-Only Sealed Assets

A copy endpoint is useful but too narrow. Evaluators often need to generate,
decompress, prepare, or run private checks. Gateway-managed private containers
provide copy as a scriptable special case without making copy the platform
abstraction.
