# CRD Reference

Complete reference for ARL-Infra Custom Resource Definitions.

## WarmPool

A WarmPool maintains a pool of pre-created pods ready for instant allocation.

### Spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `replicas` | integer | Yes | Number of pods to maintain in the pool |
| `template` | PodTemplateSpec | Yes | Pod template for pool pods |

### Status

| Field | Type | Description |
|-------|------|-------------|
| `ready` | integer | Number of ready (unallocated) pods |
| `allocated` | integer | Number of allocated pods |
| `total` | integer | Total number of pods |
| `phase` | string | Current phase (Pending, Ready, Scaling) |
| `conditions` | []Condition | Detailed status conditions |

### Example

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: WarmPool
metadata:
  name: python-pool
  namespace: default
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: executor
          image: python:3.9-slim
          command: ["sleep", "infinity"]
          resources:
            requests:
              memory: "256Mi"
              cpu: "100m"
            limits:
              memory: "512Mi"
              cpu: "500m"
          volumeMounts:
            - name: workspace
              mountPath: /workspace
      volumes:
        - name: workspace
          emptyDir: {}
```

### Pod Labels

Pods created by WarmPool have these labels:

| Label | Value | Description |
|-------|-------|-------------|
| `arl.infra.io/warmpool` | Pool name | Identifies the owning pool |
| `arl.infra.io/pod-state` | `ready` or `allocated` | Current pod state |
| `arl.infra.io/sandbox` | Session name | Set when allocated |

---

## Common Patterns

### Custom Container Image

For specialized environments:

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: WarmPool
metadata:
  name: ml-pool
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: executor
          image: pytorch/pytorch:2.0.0-cuda11.7-cudnn8-runtime
          command: ["sleep", "infinity"]
          resources:
            limits:
              nvidia.com/gpu: 1
          volumeMounts:
            - name: workspace
              mountPath: /workspace
      volumes:
        - name: workspace
          emptyDir: {}
```
