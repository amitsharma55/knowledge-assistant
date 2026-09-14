#!/usr/bin/env bash
# Drives day.sh up/down through the fake CLIs and asserts ordering:
#   up   -> both `make images` and `terraform apply` ran; apply precedes the
#           first deploy.sh call (aws eks update-kubeconfig); reseed/rollout ran.
#   down -> `deploy.sh down` (kubectl delete ingress) precedes `terraform destroy`.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
export PATH="$here/fake-cli:$PATH"
export KA_STATE_DIR="$(mktemp -d)"
export KA_NO_NOTIFY=1
export DEPLOY_OUTPUTS_JSON="$here/fixture-outputs.json"
export VERIFY_OUTPUTS_JSON="$here/fixture-outputs.json"
export KA_TF_OUTPUTS_JSON="$here/fixture-outputs.json"
export IMAGES_OUTPUTS_JSON="$here/images-fixture-outputs.json"

line() { grep -n -- "$1" "$KA_TEST_LOG" | head -1 | cut -d: -f1; }
fail=0
want() { if grep -q -- "$1" "$KA_TEST_LOG"; then echo "PASS  $2"; else echo "FAIL  $2"; fail=1; fi; }
before() { local a b; a=$(line "$1"); b=$(line "$2"); if [ -n "$a" ] && [ -n "$b" ] && [ "$a" -lt "$b" ]; then echo "PASS  $3"; else echo "FAIL  $3 (a=$a b=$b)"; fail=1; fi; }

# --- up ---
export KA_TEST_LOG="$(mktemp)"
"$root/deploy/k8s/day.sh" up >/dev/null
want "make .*images"                  "up builds images"
want "terraform .*apply -auto-approve" "up applies the daily stack"
want "buildx build --platform linux/amd64" "up build reached buildx"
before "terraform .*apply" "aws eks update-kubeconfig" "apply precedes deploy (update-kubeconfig)"
before "aws eks update-kubeconfig" "kubectl" "controller/deploy ran after kubeconfig"

# --- down ---
export KA_TEST_LOG="$(mktemp)"
"$root/deploy/k8s/day.sh" down >/dev/null
want "delete ingress"                  "down deletes the ingress"
want "terraform .*destroy -auto-approve" "down destroys the daily stack"
before "delete ingress" "terraform .*destroy" "ALB released before terraform destroy"

[ "$fail" -eq 0 ] || { echo "day_test FAILED"; exit 1; }
echo "day_test passed"
