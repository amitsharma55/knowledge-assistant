#!/usr/bin/env bash
# Runs smoke.sh against fake kubectl/curl returning healthy values; asserts it
# reports success. Proves the check logic, not a live cluster.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
export PATH="$here/fake-cli:$PATH"
out=$(mktemp)
if "$root/deploy/k8s/smoke.sh" >"$out" 2>&1 && grep -q "all smoke checks passed" "$out"; then
  echo "smoke_test passed"
else
  echo "smoke_test FAILED:"; cat "$out"; exit 1
fi
