#!/usr/bin/env bash
# Tests verify.sh against a stand-in AWS CLI, no account required.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
export PATH="$here/fake-aws:$PATH"

outputs='{
  "region": {"value": "us-east-1"},
  "verify_expectations": {"value": {
    "cluster_name": "ka-daily",
    "expected_addons": ["vpc-cni", "coredns", "kube-proxy", "eks-pod-identity-agent"],
    "node_desired_size": 1,
    "opensearch_domain": "ka-daily"
  }}
}'

echo "== healthy account passes"
VERIFY_OUTPUTS_JSON=<(printf '%s' "$outputs") \
  FAKE_CLUSTER_STATUS=ACTIVE FAKE_NODES_READY=1 \
  FAKE_ADDONS="vpc-cni coredns kube-proxy eks-pod-identity-agent" \
  FAKE_OS_STATUS=Active \
  bash "$here/../verify.sh"

echo "== missing add-on fails"
if VERIFY_OUTPUTS_JSON=<(printf '%s' "$outputs") \
  FAKE_CLUSTER_STATUS=ACTIVE FAKE_NODES_READY=1 \
  FAKE_ADDONS="vpc-cni coredns kube-proxy" \
  FAKE_OS_STATUS=Active \
  bash "$here/../verify.sh"; then
  echo "FAIL: verify.sh passed despite a missing add-on"
  exit 1
fi

echo "== OpenSearch not active fails"
# VERIFY_OS_TIMEOUT=0 makes the poll check once and give up, so a domain stuck
# non-Active fails immediately instead of waiting out the real 5-minute window.
if VERIFY_OUTPUTS_JSON=<(printf '%s' "$outputs") \
  FAKE_CLUSTER_STATUS=ACTIVE FAKE_NODES_READY=1 \
  FAKE_ADDONS="vpc-cni coredns kube-proxy eks-pod-identity-agent" \
  FAKE_OS_STATUS=Processing VERIFY_OS_TIMEOUT=0 \
  bash "$here/../verify.sh"; then
  echo "FAIL: verify.sh passed despite OpenSearch not Active"
  exit 1
fi

echo "== node group not ACTIVE fails"
if VERIFY_OUTPUTS_JSON=<(printf '%s' "$outputs") \
  FAKE_CLUSTER_STATUS=ACTIVE FAKE_NG_STATUS=CREATING \
  FAKE_ADDONS="vpc-cni coredns kube-proxy eks-pod-identity-agent" \
  FAKE_OS_STATUS=Active \
  bash "$here/../verify.sh"; then
  echo "FAIL: verify.sh passed despite the node group not ACTIVE"
  exit 1
fi

echo "ALL PASS"
