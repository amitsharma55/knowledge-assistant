#!/usr/bin/env bash
# Drives build-push.sh with fake docker/aws and a fixture; asserts an ECR login
# and a --platform linux/amd64 --push build for each of the three repos.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
export PATH="$here/fake-cli:$PATH"
export KA_TEST_LOG="$(mktemp)"
export IMAGES_OUTPUTS_JSON="$here/images-fixture-outputs.json"

"$root/deploy/docker/build-push.sh" latest >/dev/null

log=$(cat "$KA_TEST_LOG")
fail=0
check() { if grep -q -- "$1" <<<"$log"; then echo "PASS  $2"; else echo "FAIL  $2"; fail=1; fi; }
check "get-login-password"                             "ECR login attempted"
check "docker login --username AWS"                    "docker login to registry"
for svc in chat-api ingestion ui; do
  check "buildx build --platform linux/amd64 --push.*knowledge-assistant/$svc:latest" "buildx amd64 push $svc"
done
[ "$fail" -eq 0 ] || { echo "images_test FAILED"; echo "--- log ---"; echo "$log"; exit 1; }
echo "images_test passed"
