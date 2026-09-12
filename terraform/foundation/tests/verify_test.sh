#!/usr/bin/env bash
# Tests for verify.sh against the stand-in AWS CLI in tests/fake-aws. The
# point is that verify.sh fails loudly on a missing grant AND on an over-grant;
# a checker that only ever passes is worse than none.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
verify="$here/../verify.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
export PATH="$here/fake-aws:$PATH"
export VERIFY_OUTPUTS_JSON="$tmp/outputs.json"
export FAKE_AWS_RULES="$tmp/rules"
fail() { echo "FAIL: $*"; exit 1; }

chat=arn:aws:iam::123456789012:role/zz-chat-api
ingest=arn:aws:iam::123456789012:role/zz-ingestion
secret=arn:aws:secretsmanager:us-east-1:123456789012:secret:zz/anthropic-AbCdEf
model=arn:aws:bedrock:us-east-1::foundation-model
docs_object=arn:aws:s3:::zz-docs-123456789012/probe.md

cat >"$VERIFY_OUTPUTS_JSON" <<EOF
{
  "aws_region": {"value": "us-east-1"},
  "pod_role_arns": {"value": {"chat-api": "$chat", "ingestion": "$ingest", "aws-lb-controller": "arn:aws:iam::123456789012:role/zz-aws-lb-controller"}},
  "docs_bucket": {"value": "zz-docs-123456789012"},
  "anthropic_secret_arn": {"value": "$secret"},
  "verify_expectations": {"value": {
    "budget_name": "zz-monthly",
    "budget_limit_usd": 50,
    "budget_notification_count": 4,
    "project_tag": "p",
    "chat_api_model_ids": ["zz.embed-v1", "zz.chat-v1"],
    "ingestion_model_ids": ["zz.embed-v1"]
  }}
}
EOF

# Exactly the permissions the spec grants.
grant_spec() {
  cat >"$FAKE_AWS_RULES" <<EOF
$chat bedrock:InvokeModel $model/zz.embed-v1 allowed
$chat bedrock:InvokeModel $model/zz.chat-v1 allowed
$chat secretsmanager:GetSecretValue $secret allowed
$ingest bedrock:InvokeModel $model/zz.embed-v1 allowed
$ingest s3:GetObject $docs_object allowed
EOF
}

grant_spec
"$verify" >"$tmp/out" || fail "the spec's exact permissions did not pass: $(cat "$tmp/out")"
grep -q '^All checks passed.' "$tmp/out" || fail "no success line: $(cat "$tmp/out")"

# A missing grant is caught.
grant_spec
grep -v GetSecretValue "$FAKE_AWS_RULES" >"$tmp/rules.new" && mv "$tmp/rules.new" "$FAKE_AWS_RULES"
if "$verify" >"$tmp/out"; then fail "a missing secret grant passed"; fi
grep -q 'FAIL  chat-api may read the Claude key' "$tmp/out" || fail "missing grant not reported: $(cat "$tmp/out")"

# An over-grant is caught.
grant_spec
echo "$ingest secretsmanager:GetSecretValue $secret allowed" >>"$FAKE_AWS_RULES"
if "$verify" >"$tmp/out"; then fail "an over-grant passed"; fi
grep -q 'FAIL  ingestion may not read the Claude key' "$tmp/out" || fail "over-grant not reported: $(cat "$tmp/out")"

# An unconfigured-model over-grant to ingestion is caught unconditionally,
# not only via the chat-vs-ingestion model-list diff (which is empty when both
# roles are configured for the same models).
grant_spec
echo "$ingest bedrock:InvokeModel $model/anthropic.claude-sonnet-4-5-20250929-v1:0 allowed" >>"$FAKE_AWS_RULES"
if "$verify" >"$tmp/out"; then fail "an ingestion unconfigured-model over-grant passed"; fi
grep -q 'FAIL  ingestion may not invoke' "$tmp/out" || fail "ingestion unconfigured over-grant not reported: $(cat "$tmp/out")"

# A secret with no value is caught.
grant_spec
if FAKE_SECRET_EMPTY=1 "$verify" >"$tmp/out"; then fail "an empty secret passed"; fi
grep -q 'FAIL  the Claude key has no value' "$tmp/out" || fail "empty secret not reported"

# A wrong budget is caught.
if FAKE_NOTIFICATIONS=3 "$verify" >"$tmp/out"; then fail "a missing budget notification passed"; fi
if FAKE_BUDGET_LIMIT=40.0 "$verify" >"$tmp/out"; then fail "a wrong budget limit passed"; fi

# Untagged resources are caught.
if FAKE_TAGGED=0 "$verify" >"$tmp/out"; then fail "zero tagged resources passed"; fi

echo "verify.sh: all tests passed"
