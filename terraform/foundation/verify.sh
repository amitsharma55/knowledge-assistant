#!/usr/bin/env bash
# verify.sh checks the applied foundation against its spec: pod-role
# permissions through the IAM policy simulator, that the Claude key has a value
# (never printing it), the budget, and tagging. Every name and ARN comes from
# `terraform output`, so nothing configured in tfvars is repeated here. Run it
# after `terraform apply`, with the credentials Terraform used; re-run it after
# any IAM change.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)

# A Bedrock model neither role is configured for. Both must be denied it.
UNCONFIGURED_MODEL=anthropic.claude-sonnet-4-5-20250929-v1:0

if [ -n "${VERIFY_OUTPUTS_JSON:-}" ]; then
  outputs=$(cat "$VERIFY_OUTPUTS_JSON")
else
  outputs=$(terraform -chdir="$here" output -json)
fi
out() { jq -r "$1" <<<"$outputs"; }

region=$(out '.aws_region.value')
chat_role=$(out '.pod_role_arns.value["chat-api"]')
ingest_role=$(out '.pod_role_arns.value.ingestion')
docs_bucket=$(out '.docs_bucket.value')
secret_arn=$(out '.anthropic_secret_arn.value')
partition=$(cut -d: -f2 <<<"$chat_role")
account_id=$(cut -d: -f5 <<<"$chat_role")
docs_object="arn:${partition}:s3:::${docs_bucket}/probe.md"
model_arn() { echo "arn:${partition}:bedrock:${region}::foundation-model/$1"; }

failures=0
pass() { echo "PASS  $1"; }
fail() {
  echo "FAIL  $1"
  failures=$((failures + 1))
}

# expect <allowed|denied> <role-arn> <action> <resource-arn> <description>
expect() {
  local decision
  decision=$(aws iam simulate-principal-policy --policy-source-arn "$2" \
    --action-names "$3" --resource-arns "$4" \
    --query 'EvaluationResults[0].EvalDecision' --output text)
  if [ "$decision" != allowed ]; then
    decision=denied # implicitDeny and explicitDeny
  fi
  if [ "$decision" = "$1" ]; then
    pass "$5"
  else
    fail "$5 (want $1, got $decision)"
  fi
}

echo "== chat-api"
for id in $(out '.verify_expectations.value.chat_api_model_ids[]'); do
  expect allowed "$chat_role" bedrock:InvokeModel "$(model_arn "$id")" "chat-api may invoke $id"
done
expect denied "$chat_role" bedrock:InvokeModel "$(model_arn "$UNCONFIGURED_MODEL")" \
  "chat-api may not invoke $UNCONFIGURED_MODEL"
expect allowed "$chat_role" secretsmanager:GetSecretValue "$secret_arn" "chat-api may read the Claude key"
expect denied "$chat_role" s3:GetObject "$docs_object" "chat-api may not read documents"

echo "== ingestion"
for id in $(out '.verify_expectations.value.ingestion_model_ids[]'); do
  expect allowed "$ingest_role" bedrock:InvokeModel "$(model_arn "$id")" "ingestion may invoke $id"
done
for id in $(out '.verify_expectations.value | (.chat_api_model_ids - .ingestion_model_ids)[]'); do
  expect denied "$ingest_role" bedrock:InvokeModel "$(model_arn "$id")" "ingestion may not invoke $id"
done
expect denied "$ingest_role" bedrock:InvokeModel "$(model_arn "$UNCONFIGURED_MODEL")" \
  "ingestion may not invoke $UNCONFIGURED_MODEL"
expect allowed "$ingest_role" s3:GetObject "$docs_object" "ingestion may read documents"
expect denied "$ingest_role" secretsmanager:GetSecretValue "$secret_arn" "ingestion may not read the Claude key"

echo "== Claude key"
if aws secretsmanager describe-secret --secret-id "$secret_arn" \
  --query 'VersionIdsToStages' --output json | jq -e 'length > 0' >/dev/null; then
  pass "the Claude key has a value (not printed)"
else
  fail "the Claude key has no value: set it with put-secret-value"
fi

echo "== budget"
budget_name=$(out '.verify_expectations.value.budget_name')
want_limit=$(out '.verify_expectations.value.budget_limit_usd')
want_count=$(out '.verify_expectations.value.budget_notification_count')
limit=$(aws budgets describe-budget --account-id "$account_id" --budget-name "$budget_name" \
  --output json | jq -r '.Budget.BudgetLimit.Amount')
if awk -v a="$limit" -v b="$want_limit" 'BEGIN { exit !(a + 0 == b + 0) }'; then
  pass "budget limit is $want_limit"
else
  fail "budget limit is $limit, want $want_limit"
fi
count=$(aws budgets describe-notifications-for-budget --account-id "$account_id" \
  --budget-name "$budget_name" --output json | jq '.Notifications | length')
if [ "$count" -eq "$want_count" ]; then
  pass "budget has $want_count notifications"
else
  fail "budget has $count notifications, want $want_count"
fi

echo "== tags"
project=$(out '.verify_expectations.value.project_tag')
tagged=$(aws resourcegroupstaggingapi get-resources --region "$region" \
  --tag-filters "Key=Project,Values=$project" \
  --query 'length(ResourceTagMappingList)' --output text)
if [ "$tagged" -gt 0 ]; then
  pass "$tagged resources carry Project=$project"
else
  fail "no resources carry Project=$project"
fi

echo
if [ "$failures" -eq 0 ]; then
  echo "All checks passed."
else
  echo "$failures check(s) failed."
  exit 1
fi
