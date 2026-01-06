#!/bin/bash
# Debug a failing pod by collecting relevant information
# Usage: debug_pod.sh <namespace> <pod-name>

set -e

NAMESPACE=${1:-default}
POD_NAME=${2}

if [ -z "$POD_NAME" ]; then
    echo "Error: Pod name is required"
    echo "Usage: $0 <namespace> <pod-name>"
    exit 1
fi

echo "🔍 Debugging pod '$POD_NAME' in namespace '$NAMESPACE'"
echo "================================================"

# Pod status
echo -e "\n📊 Pod Status:"
kubectl get pod "$POD_NAME" -n "$NAMESPACE" -o wide

# Detailed pod info
echo -e "\n📋 Pod Details:"
kubectl describe pod "$POD_NAME" -n "$NAMESPACE"

# Container logs
echo -e "\n📜 Container Logs:"
CONTAINERS=$(kubectl get pod "$POD_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.containers[*].name}')

for CONTAINER in $CONTAINERS; do
    echo -e "\n--- Logs for container: $CONTAINER ---"
    kubectl logs "$POD_NAME" -n "$NAMESPACE" -c "$CONTAINER" --tail=100 || echo "Failed to get logs for $CONTAINER"
    
    # Previous logs if pod restarted
    echo -e "\n--- Previous logs for container: $CONTAINER (if restarted) ---"
    kubectl logs "$POD_NAME" -n "$NAMESPACE" -c "$CONTAINER" --previous --tail=100 2>/dev/null || echo "No previous logs available"
done

# Events
echo -e "\n🔔 Recent Events:"
kubectl get events -n "$NAMESPACE" --sort-by='.lastTimestamp' | grep "$POD_NAME" || echo "No events found"

echo -e "\n✅ Debug information collected"
