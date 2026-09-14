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

# `--output text` returns the addon names tab-separated on one line; split to
# one-per-line so `grep -qx` can match each exactly.
installed=$(aws eks list-addons --cluster-name "$cluster" --region "$region" \
  --query 'addons' --output text | tr '\t' '\n')
for addon in $(out '.verify_expectations.value.expected_addons[]'); do
  if echo "$installed" | grep -qx "$addon"; then
    pass "add-on $addon installed"
  else
    fail "add-on $addon missing"
  fi
done

# The module name-prefixes the node group (e.g. default-<hash>), so resolve the
# real name rather than assuming "default". A managed node group reaches ACTIVE
# only once its nodes have joined and are Ready; there is no health.readyCount
# field, so ACTIVE + the configured desired size is the readiness signal.
ng=$(aws eks list-nodegroups --cluster-name "$cluster" --region "$region" \
  --query 'nodegroups[0]' --output text)
ng_status=$(aws eks describe-nodegroup --cluster-name "$cluster" --nodegroup-name "$ng" \
  --region "$region" --query 'nodegroup.status' --output text)
desired=$(aws eks describe-nodegroup --cluster-name "$cluster" --nodegroup-name "$ng" \
  --region "$region" --query 'nodegroup.scalingConfig.desiredSize' --output text)
if [ "$ng_status" = ACTIVE ] && [ "$desired" -ge "$want_nodes" ] 2>/dev/null; then
  pass "node group $ng is ACTIVE, desired $desired (>= $want_nodes)"
else
  fail "node group $ng status=$ng_status desired=$desired, want ACTIVE and >= $want_nodes"
fi

# DomainProcessingStatus is the human-readable state (Active/Creating/Modifying);
# DomainStatus.Processing is only a bool, so it can never equal "Active".
# describe-domain can report Creating/Modifying/Processing for well over 13 min
# after apply returns (domain creation is slow, then the post-create access
# policy update settles), so poll rather than fail on the first read. The 20-min
# ceiling covers the observed worst case with margin. Overridable so the test
# harness can drive a single check (timeout 0) instead of waiting out the poll.
os_deadline=$((SECONDS + ${VERIFY_OS_TIMEOUT:-1200}))
while :; do
  os_status=$(aws opensearch describe-domain --domain-name "$os_domain" --region "$region" \
    --query 'DomainStatus.DomainProcessingStatus' --output text)
  if [ "$os_status" = Active ] || [ "$SECONDS" -ge "$os_deadline" ]; then break; fi
  echo "...   OpenSearch $os_domain is $os_status, waiting up to $((os_deadline - SECONDS))s for Active"
  sleep "${VERIFY_OS_INTERVAL:-20}"
done
if [ "$os_status" = Active ]; then pass "OpenSearch $os_domain is Active"; else fail "OpenSearch $os_domain is $os_status, want Active"; fi

if [ "$failures" -ne 0 ]; then
  echo "$failures check(s) failed"
  exit 1
fi
echo "all checks passed"
