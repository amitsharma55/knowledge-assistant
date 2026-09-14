#!/usr/bin/env bash
# Exercises day.sh reap decision logic through the fake aws (KA_FAKE_CLUSTER)
# and keep-flags. "Reaping" is detected by a terraform destroy in the log
# (reap -> down -> terraform destroy). Also plutil-lints the rendered plist.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
export PATH="$here/fake-cli:$PATH"
export KA_NO_NOTIFY=1
export KA_CLUSTER_NAME=ka-daily
export DEPLOY_OUTPUTS_JSON="$here/fixture-outputs.json"
export VERIFY_OUTPUTS_JSON="$here/fixture-outputs.json"
export KA_TF_OUTPUTS_JSON="$here/fixture-outputs.json"
fail=0
destroyed() { grep -q "terraform .*destroy" "$KA_TEST_LOG"; }

# 1. Live cluster, no keep-flag -> reaps (destroy happens).
export KA_STATE_DIR="$(mktemp -d)"; export KA_TEST_LOG="$(mktemp)"; export KA_FAKE_CLUSTER=live
"$root/deploy/k8s/day.sh" reap >/dev/null
if destroyed; then echo "PASS  reaps a live, un-kept stack"; else echo "FAIL  reaps a live, un-kept stack"; fail=1; fi

# 2. Live cluster, keep-flag for today -> skips (no destroy).
export KA_STATE_DIR="$(mktemp -d)"; export KA_TEST_LOG="$(mktemp)"; export KA_FAKE_CLUSTER=live
touch "$KA_STATE_DIR/keep-$(date +%F)"
"$root/deploy/k8s/day.sh" reap >/dev/null
if destroyed; then echo "FAIL  keep-flag prevents teardown"; fail=1; else echo "PASS  keep-flag prevents teardown"; fi

# 3. No cluster -> nothing to reap (no destroy).
export KA_STATE_DIR="$(mktemp -d)"; export KA_TEST_LOG="$(mktemp)"; export KA_FAKE_CLUSTER=absent
"$root/deploy/k8s/day.sh" reap >/dev/null
if destroyed; then echo "FAIL  absent stack is a no-op"; fail=1; else echo "PASS  absent stack is a no-op"; fi

# 4. keep subcommand writes today's flag.
export KA_STATE_DIR="$(mktemp -d)"; export KA_TEST_LOG="$(mktemp)"
"$root/deploy/k8s/day.sh" keep >/dev/null
if [ -f "$KA_STATE_DIR/keep-$(date +%F)" ]; then echo "PASS  keep writes today's flag"; else echo "FAIL  keep writes today's flag"; fail=1; fi

# 5. Rendered plist is valid (if plutil present).
if command -v plutil >/dev/null 2>&1; then
  rendered="$(mktemp)"; sed -e "s|@DAYSH@|/tmp/day.sh|g" -e "s|@HOME@|/tmp|g" \
    "$root/deploy/k8s/reaper/com.ka.daily-reaper.plist.tmpl" > "$rendered"
  if plutil -lint "$rendered" >/dev/null; then echo "PASS  plist lints"; else echo "FAIL  plist lints"; fail=1; fi
else
  echo "skip plutil (not installed)"
fi

[ "$fail" -eq 0 ] || { echo "reaper_test FAILED"; exit 1; }
echo "reaper_test passed"
