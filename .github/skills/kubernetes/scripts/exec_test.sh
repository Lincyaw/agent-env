#!/bin/bash
# Execute a command inside a pod and return the result
# Usage: exec_test.sh <namespace> <pod-name-pattern> <command>

set -e

NAMESPACE=${1:-default}
POD_PATTERN=${2}
shift 2
COMMAND="$@"

if [ -z "$POD_PATTERN" ] || [ -z "$COMMAND" ]; then
    echo "Error: Pod pattern and command are required"
    echo "Usage: $0 <namespace> <pod-name-pattern> <command>"
    exit 1
fi

# Find the first matching pod
POD_NAME=$(kubectl get pods -n "$NAMESPACE" 2>/dev/null | grep "$POD_PATTERN" | grep "Running" | awk '{print $1}' | head -n1)

if [ -z "$POD_NAME" ]; then
    echo "❌ No running pod found matching pattern '$POD_PATTERN' in namespace '$NAMESPACE'"
    exit 1
fi

echo "🔍 Executing in pod: $POD_NAME"
echo "Command: $COMMAND"
echo "---"

kubectl exec -n "$NAMESPACE" "$POD_NAME" -- $COMMAND
