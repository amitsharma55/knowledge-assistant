#!/usr/bin/env bash
# day.sh sequences the disposable daily stack across the 4a (terraform) and 4b
# (deploy.sh) layers so the owner runs one command per bookend.
#   day.sh up     build images, terraform apply, verify, deploy, print URL
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
  # Fail fast before the build, so a missing prerequisite surfaces immediately
  # rather than after a slow emulated build.
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
  # Build images BEFORE apply, not concurrently. The emulated linux/amd64 build
  # (QEMU on the arm64 laptop) raced terraform apply for the machine and lost
  # under a full fresh apply, failing the whole `up` after the stack was already
  # created and billing. Serial costs the (usually cached) build time up front
  # but cannot flake against apply. Tee to a log so a failure is never invisible.
  local build_log="$state_dir/images-build.log"
  echo "==> building images (log: $build_log)"
  if ! make -C "$root" images 2>&1 | tee "$build_log"; then
    echo "day.sh: image build failed; last 20 lines:" >&2
    tail -n 20 "$build_log" >&2
    exit 1
  fi

  echo "==> applying the daily stack"
  terraform -chdir="$daily" apply -auto-approve

  echo "==> verifying infrastructure"
  "$daily/verify.sh"

  # Upload the corpus to S3 BEFORE deploy: the reseed Job (run by deploy.sh)
  # indexes from S3, not from the fixtures on disk, so any doc added to
  # fixtures/ only reaches AWS once it is here. Skipping this is how a
  # local-only fixture ("works on my machine") silently never gets answered in
  # AWS. The bucket is the foundation docs bucket, sourced the same way
  # deploy.sh does (a daily-stack output), never hardcoded.
  echo "==> syncing the corpus to S3"
  local bucket
  bucket=$(terraform -chdir="$daily" output -json | jq -r '.docs_bucket.value')
  [ -n "$bucket" ] && [ "$bucket" != null ] || { echo "day.sh: docs_bucket output is empty" >&2; exit 1; }
  KA_DOCS_BUCKET="$bucket" make -C "$root" seed-s3

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
