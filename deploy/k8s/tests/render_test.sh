#!/usr/bin/env bash
# Renders the deploy overlay from fixture outputs (no AWS) and asserts the image
# refs and ka-config values landed. Uses kubectl kustomize (present) only.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)

export DEPLOY_OUTPUTS_JSON="$here/fixture-outputs.json"
"$root/deploy/k8s/deploy.sh" render v1.2.3

rendered=$(kubectl kustomize "$root/deploy/k8s/.generated")
fail=0
check() { if grep -q "$1" <<<"$rendered"; then echo "PASS  $2"; else echo "FAIL  $2"; fail=1; fi; }
check "knowledge-assistant/chat-api:v1.2.3"  "chat-api image rewritten with repo + tag"
check "knowledge-assistant/ui:v1.2.3"        "ui image rewritten"
check "knowledge-assistant/ingestion:v1.2.3" "ingestion image rewritten"
check "opensearch_url: https://vpc-ka-abc.us-east-1.es.amazonaws.com" "ka-config opensearch_url set with https scheme"
check "docs_bucket: ka-docs-123456789012"    "ka-config docs_bucket set"
check "name: ka-config"                      "ka-config named stably (no hash suffix)"
[ "$fail" -eq 0 ] || { echo "render_test FAILED"; exit 1; }
echo "render_test passed"
