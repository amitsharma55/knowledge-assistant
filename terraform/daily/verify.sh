#!/usr/bin/env bash
# verify.sh checks the applied daily stack against its spec: the cluster is
# ACTIVE, the expected add-ons are installed, the node group has its desired
# nodes Ready, and the OpenSearch domain is Active. Every name comes from
# `terraform output`, so nothing configured in tfvars is repeated here. Run it
# after `terraform apply`, with the credentials Terraform used.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)

if [ -n "${VERIFY_OUTPUTS_JSON:-}" ]; then
  outputs=$(cat "$VERIFY_OUTPUTS_JSON")
else
  outputs=$(terraform -chdir="$here" output -json)
fi
out() { jq -r "$1" <<<"$outputs"; }

region=$(out '.region.value')
cluster=$(out '.verify_expectations.value.cluster_name')
os_domain=$(out '.verify_expectations.value.opensearch_domain')
want_nodes=$(out '.verify_expectations.value.node_desired_size')

failures=0
pass() { echo "PASS  $1"; }
fail() {
  echo "FAIL  $1"
  failures=$((failures + 1))
}

status=$(aws eks describe-cluster --name "$cluster" --region "$region" \
  --query 'cluster.status' --output text)
if [ "$status" = ACTIVE ]; then pass "cluster $cluster is ACTIVE"; else fail "cluster $cluster is $status, want ACTIVE"; fi

installed=$(aws eks list-addons --cluster-name "$cluster" --region "$region" \
  --query 'addons' --output text)
for addon in $(out '.verify_expectations.value.expected_addons[]'); do
  if echo "$installed" | grep -qx "$addon"; then
    pass "add-on $addon installed"
  else
    fail "add-on $addon missing"
  fi
done

ready=$(aws eks describe-nodegroup --cluster-name "$cluster" --nodegroup-name default \
  --region "$region" --query 'nodegroup.health.readyCount' --output text)
if [ "$ready" -ge "$want_nodes" ] 2>/dev/null; then
  pass "node group has $ready/$want_nodes nodes ready"
else
  fail "node group has $ready ready, want $want_nodes"
fi

os_status=$(aws opensearch describe-domain --domain-name "$os_domain" --region "$region" \
  --query 'DomainStatus.Processing' --output text)
if [ "$os_status" = Active ]; then pass "OpenSearch $os_domain is Active"; else fail "OpenSearch $os_domain is $os_status, want Active"; fi

if [ "$failures" -ne 0 ]; then
  echo "$failures check(s) failed"
  exit 1
fi
echo "all checks passed"
