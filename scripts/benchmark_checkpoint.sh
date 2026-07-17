#!/usr/bin/env bash
# Benchmark checkpoint overhead across different file sizes and counts.
# Usage: ./scripts/benchmark_checkpoint.sh [GATEWAY_URL] [SESSION_ID]
#   If SESSION_ID is empty, creates a new session automatically.
set -euo pipefail

GW="${1:-http://127.0.0.1:18080}"
SID="${2:-}"
IMAGE="${IMAGE:-pair-cn-guangzhou.cr.volces.com/library/python:3.12-slim}"
RUNS="${RUNS:-3}"
PIP_MIRROR="https://pypi.tuna.tsinghua.edu.cn/simple"

exec_step() {
  local label="$1"
  local cmd="$2"
  local total=0
  for i in $(seq 1 "$RUNS"); do
    ms=$(curl -s -X POST "$GW/v1/sessions/$SID/execute" \
      -H "Content-Type: application/json" \
      -d "{\"steps\":[{\"command\":[\"sh\",\"-c\",$(printf '%s' "$cmd" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read()))')]}]}" \
      | python3 -c "import json,sys;print(json.load(sys.stdin)['results'][0]['duration_ms'])")
    total=$((total + ms))
    printf "  run %d: %dms\n" "$i" "$ms"
  done
  avg=$((total / RUNS))
  printf "  >> %s avg: %dms\n\n" "$label" "$avg"
}

if [ -z "$SID" ]; then
  echo "Creating session with image $IMAGE ..."
  SID=$(curl -s -X POST "$GW/v1/sessions" -H "Content-Type: application/json" \
    -d "{\"image\":\"$IMAGE\"}" | python3 -c "import json,sys;print(json.load(sys.stdin)['id'])")
  echo "Session: $SID"
  echo ""
fi

echo "Gateway: $GW"
echo "Session: $SID"
echo "Runs per test: $RUNS"
echo "============================================"

echo "--- Baseline: echo (no file changes) ---"
exec_step "baseline" "echo hello"

echo "--- 1 small file (100B) ---"
exec_step "100B x1" "dd if=/dev/urandom bs=100 count=1 of=/tmp/b_small.bin 2>/dev/null"

echo "--- 1 medium file (1MB) ---"
exec_step "1MB x1" "dd if=/dev/urandom bs=1M count=1 of=/tmp/b_med.bin 2>/dev/null"

echo "--- 1 large file (10MB) ---"
exec_step "10MB x1" "dd if=/dev/urandom bs=1M count=10 of=/tmp/b_10m.bin 2>/dev/null"

echo "--- 1 large file (100MB) ---"
exec_step "100MB x1" "dd if=/dev/urandom bs=1M count=100 of=/tmp/b_100m.bin 2>/dev/null"

echo "--- 100 small files (1KB each, 100KB total) ---"
exec_step "1KB x100" "for i in \$(seq 1 100); do dd if=/dev/urandom bs=1024 count=1 of=/tmp/b_f\$i.bin 2>/dev/null; done"

echo "--- 1000 small files (1KB each, 1MB total) ---"
exec_step "1KB x1000" "mkdir -p /tmp/b_1k && for i in \$(seq 1 1000); do dd if=/dev/urandom bs=1024 count=1 of=/tmp/b_1k/f\$i.bin 2>/dev/null; done"

echo "--- 10000 small files (1KB each, 10MB total) ---"
exec_step "1KB x10000" "mkdir -p /tmp/b_10k && for i in \$(seq 1 10000); do dd if=/dev/urandom bs=1024 count=1 of=/tmp/b_10k/f\$i.bin 2>/dev/null; done"

echo "--- pip install requests (real-world, using tsinghua mirror) ---"
exec_step "pip-requests" "pip install requests -q -i $PIP_MIRROR 2>&1 | tail -1"

echo "--- pip install flask (real-world, using tsinghua mirror) ---"
exec_step "pip-flask" "pip install flask -q -i $PIP_MIRROR 2>&1 | tail -1"

echo "--- pip install numpy (large package, using tsinghua mirror) ---"
exec_step "pip-numpy" "pip install numpy -q -i $PIP_MIRROR 2>&1 | tail -1"

echo "--- Fork from step 2 (combined tar download + upload + extract) ---"
for i in $(seq 1 "$RUNS"); do
  start=$(date +%s%N)
  FORK=$(curl -s -X POST "$GW/v1/sessions/$SID/fork" -H "Content-Type: application/json" -d '{"step":2}')
  end=$(date +%s%N)
  ms=$(( (end - start) / 1000000 ))
  fork_id=$(echo "$FORK" | python3 -c "import json,sys;print(json.load(sys.stdin).get('session',{}).get('id','FAILED'))" 2>/dev/null)
  printf "  run %d: %dms (fork_session=%s)\n" "$i" "$ms" "$fork_id"
  # cleanup fork session
  if [ "$fork_id" != "FAILED" ]; then
    curl -s -X DELETE "$GW/v1/sessions/$fork_id" > /dev/null 2>&1 || true
  fi
done

echo ""
echo "============================================"
echo "Benchmark complete."
