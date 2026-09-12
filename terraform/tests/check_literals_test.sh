#!/usr/bin/env bash
# Tests for check-literals.sh. Each case builds a throwaway stack directory.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
check="$here/../check-literals.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $*"; exit 1; }

# A value that is in tfvars and also written in code is caught, with file:line.
mkdir -p "$tmp/leaky"
printf 'region = "zz-region-1"\n' >"$tmp/leaky/terraform.tfvars"
printf 'provider "aws" {\n  region = "zz-region-1"\n}\n' >"$tmp/leaky/main.tf"
if out=$("$check" "$tmp/leaky" 2>&1); then fail "leaky stack passed"; fi
grep -q 'main.tf:2:' <<<"$out" || fail "hit not reported as file:line: $out"

# The same value referenced through a variable passes, and a variable whose
# name contains a value's text does not trip the check.
mkdir -p "$tmp/clean"
printf 'zz_region = "zz-region-1"\nzz_secret_name = "zz/secret"\n' >"$tmp/clean/terraform.tfvars"
printf 'variable "zz_region" {\n  type = string\n}\nvariable "zz_secret_name" {\n  type = string\n}\nprovider "aws" {\n  region = var.zz_region\n}\n' >"$tmp/clean/main.tf"
"$check" "$tmp/clean" >/dev/null || fail "clean stack failed"

# Values from private *.auto.tfvars count too.
mkdir -p "$tmp/private"
printf 'x = "a"\n' >"$tmp/private/terraform.tfvars"
printf 'alert = "zz@example.test"\n' >"$tmp/private/private.auto.tfvars"
printf 'locals {\n  e = "zz@example.test"\n}\n' >"$tmp/private/main.tf"
if "$check" "$tmp/private" >/dev/null 2>&1; then fail "private literal passed"; fi

# A directory without tfvars (a module) is skipped.
mkdir -p "$tmp/module"
printf 'locals {\n  a = "anything"\n}\n' >"$tmp/module/main.tf"
"$check" "$tmp/module" >/dev/null || fail "module without tfvars failed"

echo "check-literals: all tests passed"
