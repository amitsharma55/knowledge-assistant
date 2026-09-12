#!/usr/bin/env bash
# check.sh runs every offline check on the Terraform code: formatting,
# validation, `terraform test` against a mocked AWS provider, the rule that
# configuration lives in tfvars (check-literals.sh), and the shell script
# tests. It needs no AWS credentials and never touches an account or the S3
# backend. Paths are assumed to contain no spaces.
set -euo pipefail

root=$(cd "$(dirname "$0")" && pwd)

echo "== terraform fmt"
terraform fmt -check -recursive "$root"

dirs=""
for d in "$root"/*/ "$root"/modules/*/; do
  if [ -f "$d/versions.tf" ]; then
    dirs="$dirs ${d%/}"
  fi
done

for d in $dirs; do
  echo "== ${d#"$root"/}"
  terraform -chdir="$d" init -backend=false -input=false >/dev/null
  terraform -chdir="$d" validate
  if compgen -G "$d/tests/*.tftest.hcl" >/dev/null; then
    terraform -chdir="$d" test
  fi
done

echo "== configuration lives in tfvars"
# $dirs is deliberately unquoted: a space-separated list of paths.
"$root/check-literals.sh" $dirs

echo "== the Claude key never enters Terraform"
if grep -rn --include='*.tf' 'aws_secretsmanager_secret_version' "$root"; then
  echo "aws_secretsmanager_secret_version would put the key in state; set it with put-secret-value instead." >&2
  exit 1
fi

echo "== script tests"
for t in "$root"/tests/*_test.sh "$root"/*/tests/*_test.sh "$root"/modules/*/tests/*_test.sh; do
  if [ -f "$t" ]; then
    bash "$t"
  fi
done

echo "All Terraform checks passed."
