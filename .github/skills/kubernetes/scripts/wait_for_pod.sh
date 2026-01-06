#!/bin/bash
# Wait for a pod to reach a specific status
# Usage: wait_for_pod.sh <namespace> <pod-name-pattern> <timeout-seconds>

set -e

NAMESPACE=${1:-default}
POD_PATTERN=${2}
TIMEOUT=${3:-300}

if [ -z "$POD_PATTERN" ]; then
    echo "Error: Pod name pattern is required"
    echo "Usage: $0 <namespace> <pod-name-pattern> <timeout-seconds>"
    exit 1
fi

echo "Waiting for pod matching '$POD_PATTERN' in namespace '$NAMESPACE' to be ready..."

END_TIME=$(($(date +%s) + TIMEOUT))

while [ $(date +%s) -lt $END_TIME ]; do
    POD_STATUS=$(kubectl get pods -n "$NAMESPACE" 2>/dev/null | grep "$POD_PATTERN" | awk '{print $3}' | head -n1)
    
    if [ "$POD_STATUS" == "Running" ]; then
        POD_NAME=$(kubectl get pods -n "$NAMESPACE" 2>/dev/null | grep "$POD_PATTERN" | awk '{print $1}' | head -n1)
        # Check if all containers are ready
        READY=$(kubectl get pod "$POD_NAME" -n "$NAMESPACE" -o jsonpath='{.status.containerStatuses[0].ready}')
        if [ "$READY" == "true" ]; then
            echo "✅ Pod $POD_NAME is ready"
            exit 0
        fi
    fi
    
    echo "Pod status: $POD_STATUS, waiting..."
    sleep 5
done

echo "❌ Timeout waiting for pod to be ready"
exit 1
