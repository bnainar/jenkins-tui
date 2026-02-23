#!/usr/bin/env bash
set -euo pipefail

url="${1:-http://localhost:8080/login}"

for _ in $(seq 1 90); do
  if curl -fsS "$url" >/dev/null 2>&1; then
    echo "Jenkins is ready"
    exit 0
  fi
  sleep 2
done

echo "Timed out waiting for Jenkins" >&2
exit 1
