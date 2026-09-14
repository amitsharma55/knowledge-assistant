#!/usr/bin/env bash
# Offline validation for the 4b deploy assets. Uses only kubectl + jq + bash;
# additionally runs shellcheck and kubeconform when installed. No AWS, no cluster.
# Note: `kubectl apply --dry-run=client` is NOT used -- it contacts the cluster
# to resolve resource kinds. `kubectl kustomize` renders fully offline; install
# kubeconform for offline schema validation.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)

echo "==> base manifests render (kubectl kustomize)"
kubectl kustomize "$here/base" >/dev/null
echo "PASS base renders"

echo "==> shell syntax (bash -n)"
for s in "$here"/*.sh "$here"/tests/*.sh "$here"/tests/fake-cli/* "$here"/../docker/build-push.sh; do bash -n "$s"; done
echo "PASS bash -n"

echo "==> overlay render test"; "$here/tests/render_test.sh"
echo "==> smoke logic test";   "$here/tests/smoke_test.sh"
echo "==> images build test"; "$here/tests/images_test.sh"
echo "==> day orchestration test"; "$here/tests/day_test.sh"

if command -v shellcheck >/dev/null 2>&1; then
  echo "==> shellcheck"
  shellcheck "$here"/*.sh "$here"/tests/*.sh "$here"/tests/fake-cli/* "$here"/../docker/build-push.sh && echo "PASS shellcheck"
else
  echo "skip shellcheck (not installed; brew install shellcheck for a stronger check)"
fi

if command -v kubeconform >/dev/null 2>&1; then
  echo "==> kubeconform"
  kubectl kustomize "$here/base" | kubeconform -strict -ignore-missing-schemas - && echo "PASS kubeconform"
else
  echo "skip kubeconform (not installed; brew install kubeconform for schema validation)"
fi

echo "all k8s checks passed"
