#!/usr/bin/env bash
# day.sh sequences the disposable daily stack across the 4a (terraform) and 4b
# (deploy.sh) layers so the owner runs one command per bookend.
#   day.sh up     build images ‖ terraform apply, verify, deploy, print URL
#   day.sh down   deploy.sh down (release ALB), then terraform destroy
# terraform runs with -auto-approve on every path: only the disposable daily
# stack is ever touched; the corpus lives in foundation's S3.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
daily="$root/terraform/daily"
state_dir="${KA_STATE_DIR:-$HOME/.ka}"
mkdir -p "$state_dir"

preflight() {
  # Fail fast BEFORE launching the background build, so a missing prerequisite
  # can't leave an orphaned build interleaving with a terraform error.
  [ -n "${KA_SKIP_PREFLIGHT:-}" ] && return 0
  local t missing=""
  for t in docker terraform aws helm kubectl jq make; do
    command -v "$t" >/dev/null 2>&1 || missing="$missing $t"
  done
  if [ -n "$missing" ]; then
    echo "day.sh: missing required tools:$missing" >&2
    exit 1
  fi
  # The daily stack must be initialized against its S3 backend. That leaves a
  # .terraform/terraform.tfstate marker; its absence means `terraform apply`
  # would abort with "Backend initialization required".
  if [ ! -f "$daily/.terraform/terraform.tfstate" ]; then
    echo "day.sh: the daily stack is not initialized against its S3 backend." >&2
    echo "  Set it up once, then re-run 'day.sh up':" >&2
    echo "    cd $daily" >&2
    echo "    cp backend.hcl.example backend.hcl              # edit values" >&2
    echo "    cp private.auto.tfvars.example private.auto.tfvars  # edit values" >&2
    echo "    terraform init -backend-config=backend.hcl" >&2
    exit 1
  fi
}

up() {
  preflight
  echo "==> building images ‖ applying the daily stack"
  make -C "$root" images &
  local build_pid=$!
  # If apply fails, stop the background build rather than orphaning it.
  if ! terraform -chdir="$daily" apply -auto-approve; then
    echo "day.sh: terraform apply failed; stopping the image build" >&2
    kill "$build_pid" 2>/dev/null || true
    wait "$build_pid" 2>/dev/null || true
    exit 1
  fi
  if ! wait "$build_pid"; then echo "day.sh: image build failed" >&2; exit 1; fi

  echo "==> verifying infrastructure"
  "$daily/verify.sh"

  echo "==> deploying the app"
  "$here/deploy.sh" up latest
}

down() {
  echo "==> releasing the ALB and workloads (must precede terraform destroy)"
  "$here/deploy.sh" down
  echo "==> destroying the daily stack"
  terraform -chdir="$daily" destroy -auto-approve
  rm -f "$state_dir"/keep-* 2>/dev/null || true
  echo "daily stack destroyed."
}

notify() { # <message>
  local msg="$1"
  echo "$(date '+%F %T') $msg" >> "$state_dir/reaper.log"
  [ -n "${KA_NO_NOTIFY:-}" ] && return 0
  if command -v terminal-notifier >/dev/null 2>&1; then
    terminal-notifier -title "KA daily reaper" -message "$msg" >/dev/null 2>&1 || true
  elif command -v osascript >/dev/null 2>&1; then
    osascript -e "display notification \"$msg\" with title \"KA daily reaper\"" >/dev/null 2>&1 || true
  fi
}

keep() {
  local flag="$state_dir/keep-$(date +%F)"
  touch "$flag"
  echo "keep flag set for today ($flag); tonight's reaper will skip teardown."
}

reap() {
  if [ -f "$state_dir/keep-$(date +%F)" ]; then
    notify "daily stack kept for today; skipping teardown."
    return 0
  fi
  local name
  name="${KA_CLUSTER_NAME:-$(terraform -chdir="$daily" output -raw cluster_name 2>/dev/null || true)}"
  if [ -z "$name" ]; then
    notify "no daily cluster name resolvable; nothing to reap."
    return 0
  fi
  if aws eks describe-cluster --name "$name" >/dev/null 2>&1; then
    notify "daily stack ($name) still up at cutoff; reaping."
    down
  else
    notify "daily cluster ($name) not found; nothing to reap."
  fi
}

cmd="${1:-}"
shift || true
case "$cmd" in
  up)   up ;;
  down) down ;;
  reap) reap ;;
  keep) keep ;;
  *) echo "usage: day.sh [up|down|reap|keep]" >&2; exit 2 ;;
esac
