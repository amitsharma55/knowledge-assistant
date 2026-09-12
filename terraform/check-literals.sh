#!/usr/bin/env bash
# check-literals.sh <stack-dir>... fails when a quoted value from a stack's
# tfvars files also appears in that stack's .tf files. Configuration belongs in
# tfvars; a value written in both places is a value that will one day be
# changed in only one of them. Matching whole quoted strings means a variable
# name such as anthropic_secret_name never matches its value.
#
# A value that happens to equal a quoted label in code (a resource or variable
# name) is reported too. That is a false positive, but a loud one: rename the
# label.
set -euo pipefail

status=0
for dir in "$@"; do
  patterns=$(cat "$dir"/terraform.tfvars "$dir"/*.auto.tfvars 2>/dev/null |
    grep -oE '"[^"]+"' | sort -u || true)
  if [ -z "$patterns" ]; then
    continue
  fi
  for tf in "$dir"/*.tf; do
    if [ ! -f "$tf" ]; then
      continue
    fi
    if grep -nF -f <(printf '%s\n' "$patterns") "$tf" | sed "s|^|$tf:|"; then
      status=1
    fi
  done
done

if [ "$status" -eq 0 ]; then
  echo "No tfvars values found in .tf files."
fi
exit "$status"
