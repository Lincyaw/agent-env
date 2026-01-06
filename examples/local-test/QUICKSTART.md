# Quick Start - 5 Minutes to Running Examples

Complete setup and testing in 3 simple steps.

## Prerequisites Check

```bash
# Verify you have these installed
docker --version
minikube version
uv --version  # Install: curl -LsSf https://astral.sh/uv/install.sh | sh
```

## Step 1: Setup (2 min)

```bash
cd examples/local-test
./setup-minikube.sh
```

This will:
- ✅ Start minikube cluster
- ✅ Build operator and sidecar images

## Step 2: Deploy (1 min)

```bash
./deploy.sh
```

This will:
- ✅ Deploy CRDs and operator
- ✅ Create WarmPool with 2 ready pods

## Step 3: Test (2 min)

```bash
./run-examples.sh
```

This will:
- ✅ Install Python dependencies
- ✅ Run all 7 examples
- ✅ Show test results

## Expected Output

```
=== Test Results ===

Passed: 7/7
  ✓ 01_basic_execution.py
  ✓ 02_multi_step_pipeline.py
  ✓ 03_environment_variables.py
  ✓ 04_working_directory.py
  ✓ 05_error_handling.py
  ✓ 06_long_running_task.py
  ✓ 07_sandbox_reuse.py

All examples passed successfully! 🎉
```

## Cleanup

```bash
./cleanup.sh
```

## Troubleshooting

If anything fails:

1. Check minikube status: `minikube status`
2. View operator logs: `minikube kubectl -- logs -n arl-system -l app=arl-operator`
3. Check pods: `minikube kubectl -- get pods -A`

See [README.md](README.md) for detailed troubleshooting.

## What's Next?

- Modify examples in `../python/`
- Create your own sandboxes and tasks
- See [User Manual](../../docs/user-manual.md) for API details
