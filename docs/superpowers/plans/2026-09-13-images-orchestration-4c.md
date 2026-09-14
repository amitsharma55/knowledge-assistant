# Images & Orchestration (Sub-project 4c) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and push the three service images to ECR for `linux/amd64`, wrap the cross-layer daily lifecycle in a single `day.sh up|down` orchestrator, and install a macOS launchd reaper that auto-tears-down the daily stack at 22:00 America/Chicago if teardown is forgotten — all offline-validated with no AWS and no cluster.

**Architecture:** Three shell components on the owner's laptop. `deploy/docker/build-push.sh` (invoked by `make images`) reads the ECR registry from the persistent *foundation* stack and `docker buildx build --platform linux/amd64 --push`es all three images. `deploy/k8s/day.sh` sequences the day: `up` runs `make images` in the background while `terraform -chdir=terraform/daily apply -auto-approve` runs in the foreground, waits for both, runs `verify.sh`, then `deploy.sh up latest`; `down` runs `deploy.sh down` then `terraform ... destroy -auto-approve`; `reap` teardowns a still-live stack unless a per-day keep-flag is set; `keep` sets that flag. `deploy/k8s/reaper/install.sh` renders a LaunchAgent plist (`StartCalendarInterval` 22:00 local) and `launchctl load`s it. Everything validates offline through the existing `deploy/k8s/check.sh` fake-CLI + fixture pattern.

**Tech Stack:** bash + jq, Docker with buildx (QEMU for cross-arch), AWS CLI v2, Terraform 1.16.2 (invoked, not modified), Helm + kubectl (invoked via the 4b `deploy.sh`), macOS `launchctl`/`plutil`. Optional stronger offline checks: `shellcheck` (brew). No Go, no Terraform, no manifest changes.

**Spec:** `docs/superpowers/specs/2026-09-13-images-orchestration-4c-design.md`

## Global Constraints

- **Cross-arch is mandatory.** The laptop is arm64; nodes are `t3.medium` (amd64). Every image build passes `--platform linux/amd64`. Platform overridable via `KA_IMAGE_PLATFORM` (default `linux/amd64`) for tests only.
- **Registry source is FOUNDATION, not daily.** `build-push.sh` reads `terraform -chdir=terraform/foundation output -json`. Foundation output names (exact): `aws_region` (string) and `ecr_repository_urls` (map(string)). Override with `IMAGES_OUTPUTS_JSON` for offline tests. (The daily stack's own outputs are `region` and a pass-through `ecr_repository_urls`; do not read daily during a build — it may be mid-apply.)
- **ECR map keys** (foundation `ecr_repository_urls`, keyed by full repo name): `knowledge-assistant/chat-api`, `knowledge-assistant/ingestion`, `knowledge-assistant/ui`. Each value is the full repo URI `<acct>.dkr.ecr.<region>.amazonaws.com/knowledge-assistant/<svc>`; the registry host is the value up to the first `/`.
- **Image tag is `latest`.** Rebuilt every bring-up; safe because each morning's cluster is new and pulls fresh. Tag overridable as `build-push.sh [tag]` (default `latest`); `day.sh up` uses `latest` and calls `deploy.sh up latest`.
- **`-auto-approve` on every terraform path** (`up` apply, `down`/`reap` destroy). Deliberate and safe: only `terraform -chdir=terraform/daily` is ever applied/destroyed — foundation (S3 corpus, IAM, ECR, secrets) is never touched. Never target foundation from any 4c script.
- **Teardown ordering is load-bearing.** `down` runs `deploy.sh down` (releases the ALB + ENIs) *before* `terraform ... destroy`, or subnet/ENI deletes hang. The reaper reuses `day.sh down`, so it inherits this ordering.
- **State/flag dir is overridable.** `day.sh` and the reaper keep the keep-flags and log under `KA_STATE_DIR` (default `$HOME/.ka`). Keep-flag file: `$KA_STATE_DIR/keep-<YYYY-MM-DD>`. Reaper log: `$KA_STATE_DIR/reaper.log`. Tests point `KA_STATE_DIR` at a temp dir.
- **Notifications never fire in tests.** A `notify()` helper is a no-op when `KA_NO_NOTIFY` is set; tests set it. Live, it uses `terminal-notifier` if present, else `osascript`, and always appends to the reaper log.
- **Sibling scripts by path; CLI tools by name.** `day.sh` calls `deploy.sh`/`verify.sh` by absolute path and `terraform`/`aws`/`helm`/`kubectl`/`make`/`docker` by bare name (so a fake-CLI PATH shim intercepts them). Do not hardcode tool paths.
- **Offline gate: `make k8s-check` must stay green with only `bash`, `jq`, `kubectl`.** `shellcheck` runs if installed. New scripts join `check.sh`'s `bash -n`/`shellcheck` globs; new tests are invoked explicitly by `check.sh`. No test may require AWS, Docker, a cluster, or `launchctl`.
- **Existing fake-CLI behavior to preserve** (`deploy/k8s/tests/fake-cli/`): `kubectl` echoes `1` for `readyReplicas`/`status.succeeded`, the ALB hostname for `loadBalancer…hostname`, else empty; `curl` echoes `200`. New stubs must not break `render_test.sh` or `smoke_test.sh`.

---

## Task 1: Image build & push (`make images`)

**Files:**
- Create: `deploy/docker/build-push.sh`
- Create: `deploy/k8s/tests/images_test.sh`
- Create: `deploy/k8s/tests/images-fixture-outputs.json`
- Create: `deploy/k8s/tests/fake-cli/docker`
- Create: `deploy/k8s/tests/fake-cli/aws`
- Modify: `Makefile` (add `images` target + `.PHONY`)
- Modify: `deploy/k8s/check.sh` (shellcheck/`bash -n` cover `build-push.sh`; run `images_test.sh`)

**Interfaces:**
- Produces: `deploy/docker/build-push.sh [tag]` — reads foundation `aws_region` + `ecr_repository_urls` (or `IMAGES_OUTPUTS_JSON`), `docker login`s to the ECR registry, and `docker buildx build --platform "$KA_IMAGE_PLATFORM" --push -t <repo>:<tag>` for chat-api, ingestion, ui from repo root. `make images` calls it with no args (tag `latest`).
- Produces (test contract): the `fake-cli/aws` and `fake-cli/docker` stubs, reused by Tasks 2–3. Both append `"<tool> <args>"` to `$KA_TEST_LOG` when set. `aws` honors `KA_FAKE_CLUSTER` (`live` default / `absent`) for `eks describe-cluster` and returns verify-friendly values for the other `aws` queries (see stub body).

- [ ] **Step 1: Write the images fixture**

Create `deploy/k8s/tests/images-fixture-outputs.json` (foundation shape — note `aws_region`, not `region`):

```json
{
  "aws_region": {"value": "us-east-1"},
  "ecr_repository_urls": {"value": {
    "knowledge-assistant/chat-api": "123456789012.dkr.ecr.us-east-1.amazonaws.com/knowledge-assistant/chat-api",
    "knowledge-assistant/ui": "123456789012.dkr.ecr.us-east-1.amazonaws.com/knowledge-assistant/ui",
    "knowledge-assistant/ingestion": "123456789012.dkr.ecr.us-east-1.amazonaws.com/knowledge-assistant/ingestion"
  }}
}
```

- [ ] **Step 2: Write the shared fake `aws` stub**

Create `deploy/k8s/tests/fake-cli/aws` and `chmod +x` it. This stub serves all three tasks; write it in full now:

```bash
#!/usr/bin/env bash
# Fake aws for offline tests. Logs every call; returns canned, verify-friendly
# values. `eks describe-cluster` existence is controlled by KA_FAKE_CLUSTER.
echo "aws $*" >> "${KA_TEST_LOG:-/dev/null}"
case "$*" in
  *"ecr get-login-password"*)   echo "fake-ecr-password" ;;
  *"eks describe-cluster"*)
    if [ "${KA_FAKE_CLUSTER:-live}" = absent ]; then exit 254; fi
    # verify.sh queries cluster.status; reap only checks exit code.
    echo ACTIVE ;;
  *"eks list-addons"*)          printf 'vpc-cni\ncoredns\nkube-proxy\neks-pod-identity-agent\n' ;;
  *"eks describe-nodegroup"*)   echo 1 ;;
  *"opensearch describe-domain"*) echo Active ;;
  *"eks update-kubeconfig"*)    : ;;
  *) : ;;
esac
```

- [ ] **Step 3: Write the shared fake `docker` stub**

Create `deploy/k8s/tests/fake-cli/docker` and `chmod +x` it:

```bash
#!/usr/bin/env bash
# Fake docker for offline tests. Logs every call; buildx inspect fails so the
# script exercises the create-builder path; everything else succeeds.
echo "docker $*" >> "${KA_TEST_LOG:-/dev/null}"
case "$*" in
  *"buildx inspect"*) exit 1 ;;
  *) : ;;
esac
```

- [ ] **Step 4: Write the failing test**

Create `deploy/k8s/tests/images_test.sh` and `chmod +x` it:

```bash
#!/usr/bin/env bash
# Drives build-push.sh with fake docker/aws and a fixture; asserts an ECR login
# and a --platform linux/amd64 --push build for each of the three repos.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
export PATH="$here/fake-cli:$PATH"
export KA_TEST_LOG="$(mktemp)"
export IMAGES_OUTPUTS_JSON="$here/images-fixture-outputs.json"

"$root/deploy/docker/build-push.sh" latest >/dev/null

log=$(cat "$KA_TEST_LOG")
fail=0
check() { if grep -q -- "$1" <<<"$log"; then echo "PASS  $2"; else echo "FAIL  $2"; fail=1; fi; }
check "get-login-password"                             "ECR login attempted"
check "docker login --username AWS"                    "docker login to registry"
for svc in chat-api ingestion ui; do
  check "buildx build --platform linux/amd64 --push.*knowledge-assistant/$svc:latest" "buildx amd64 push $svc"
done
[ "$fail" -eq 0 ] || { echo "images_test FAILED"; echo "--- log ---"; echo "$log"; exit 1; }
echo "images_test passed"
```

- [ ] **Step 5: Run the test to verify it fails**

Run: `deploy/k8s/tests/images_test.sh`
Expected: FAIL — `build-push.sh` does not exist yet (`No such file or directory`).

- [ ] **Step 6: Write `build-push.sh`**

Create `deploy/docker/build-push.sh` and `chmod +x` it:

```bash
#!/usr/bin/env bash
# Build the three service images for linux/amd64 and push them to ECR.
# Runs on the owner's arm64 laptop; nodes are amd64, so --platform is mandatory
# (the Go build stages run under emulation, hidden behind `terraform apply`).
# The registry comes from FOUNDATION, not the daily stack: builds run in
# parallel with `terraform apply` on daily, so daily outputs are mid-flux, while
# ECR is persistent foundation infrastructure.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
foundation="$root/terraform/foundation"
tag="${1:-latest}"
platform="${KA_IMAGE_PLATFORM:-linux/amd64}"

outputs_json() {
  if [ -n "${IMAGES_OUTPUTS_JSON:-}" ]; then
    cat "$IMAGES_OUTPUTS_JSON"
  else
    terraform -chdir="$foundation" output -json
  fi
}
outputs=$(outputs_json)
out() { jq -r "$1" <<<"$outputs"; }
region=$(out '.aws_region.value')
chat=$(out '.ecr_repository_urls.value["knowledge-assistant/chat-api"]')
ingest=$(out '.ecr_repository_urls.value["knowledge-assistant/ingestion"]')
ui=$(out '.ecr_repository_urls.value["knowledge-assistant/ui"]')
registry="${chat%%/*}"   # <acct>.dkr.ecr.<region>.amazonaws.com

aws ecr get-login-password --region "$region" \
  | docker login --username AWS --password-stdin "$registry"

# A buildx builder that can target linux/amd64 (via QEMU) must exist. Reuse one
# named ka-builder across runs; create it the first time.
docker buildx inspect ka-builder >/dev/null 2>&1 || docker buildx create --name ka-builder --use >/dev/null
docker buildx use ka-builder

build() { # <repo-uri> <dockerfile>
  docker buildx build --platform "$platform" --push \
    -t "$1:$tag" -f "$2" "$root"
}
build "$chat"   "$root/deploy/docker/chat-api.Dockerfile"
build "$ingest" "$root/deploy/docker/ingestion.Dockerfile"
build "$ui"     "$root/deploy/docker/ui.Dockerfile"

echo "pushed chat-api, ingestion, ui to $registry (platform $platform, tag $tag)"
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `deploy/k8s/tests/images_test.sh`
Expected: PASS — all five checks green, `images_test passed`.

- [ ] **Step 8: Add the `make images` target**

In `Makefile`, add `images` to the `.PHONY` line and add the target near `build:`:

```makefile
# Owner-run: needs Docker (buildx) + AWS credentials. Builds all three service
# images for linux/amd64 and pushes them to foundation's ECR, tagged latest.
# Reads the registry from the foundation stack's outputs.
images:
	deploy/docker/build-push.sh
```

- [ ] **Step 9: Wire `build-push.sh` + `images_test.sh` into `check.sh`**

In `deploy/k8s/check.sh`: (a) add `build-push.sh` to the `bash -n` loop and the `shellcheck` invocation, and (b) run the new test. Change the `bash -n` loop to also cover the docker script:

```bash
echo "==> shell syntax (bash -n)"
for s in "$here"/*.sh "$here"/tests/*.sh "$here"/tests/fake-cli/* "$here"/../docker/build-push.sh; do bash -n "$s"; done
echo "PASS bash -n"
```

Add after the smoke logic test line:

```bash
echo "==> images build test"; "$here/tests/images_test.sh"
```

And extend the `shellcheck` line to include the docker script:

```bash
  shellcheck "$here"/*.sh "$here"/tests/*.sh "$here"/tests/fake-cli/* "$here"/../docker/build-push.sh && echo "PASS shellcheck"
```

- [ ] **Step 10: Run the full offline gate**

Run: `make k8s-check`
Expected: PASS through `images build test` (and `shellcheck` if installed); `all k8s checks passed`.

- [ ] **Step 11: Commit**

```bash
git add deploy/docker/build-push.sh deploy/k8s/tests/images_test.sh \
  deploy/k8s/tests/images-fixture-outputs.json deploy/k8s/tests/fake-cli/docker \
  deploy/k8s/tests/fake-cli/aws Makefile deploy/k8s/check.sh
git commit -m "Add make images: cross-arch build and push to ECR"
```

---

## Task 2: Daily orchestrator (`day.sh up|down`)

**Files:**
- Create: `deploy/k8s/day.sh`
- Create: `deploy/k8s/tests/day_test.sh`
- Create: `deploy/k8s/tests/fake-cli/terraform`
- Create: `deploy/k8s/tests/fake-cli/helm`
- Create: `deploy/k8s/tests/fake-cli/make`
- Modify: `deploy/k8s/tests/fixture-outputs.json` (add `verify_expectations` so `verify.sh` passes under fakes)
- Modify: `deploy/k8s/check.sh` (run `day_test.sh`)

**Interfaces:**
- Consumes: `deploy/docker/build-push.sh` via `make images` (Task 1); the shared `fake-cli/aws`, `fake-cli/docker` (Task 1); the 4b `deploy.sh up latest` / `deploy.sh down` and `terraform/daily/verify.sh`.
- Produces: `deploy/k8s/day.sh <up|down|reap|keep>`. This task implements `up` and `down`; Task 3 adds `reap` and `keep`. `up`: `make images` (background) ‖ `terraform -chdir=<daily> apply -auto-approve` (foreground), `wait`, fail on either error, `verify.sh`, `deploy.sh up latest`, print URL. `down`: `deploy.sh down`, `terraform -chdir=<daily> destroy -auto-approve`, clear keep-flags. Provides shell functions `up`, `down` and (Task 3) `reap`, `keep`; dispatched by a trailing `case`.
- Produces (env contract): honors `KA_STATE_DIR` (default `$HOME/.ka`), and `DEPLOY_OUTPUTS_JSON`/`VERIFY_OUTPUTS_JSON` pass through to the sibling scripts so tests avoid live `terraform output`.

- [ ] **Step 1: Add `verify_expectations` to the deploy fixture**

`verify.sh` (called by `day.sh up`) reads `verify_expectations` from the outputs. Add it to `deploy/k8s/tests/fixture-outputs.json` (keep the existing keys; `render_test.sh` greps specific keys and is unaffected):

```json
  ,"node_group_name": {"value": "default"},
  "verify_expectations": {"value": {
    "cluster_name": "ka-daily",
    "opensearch_domain": "ka-daily-os",
    "node_desired_size": 1,
    "expected_addons": ["vpc-cni", "coredns", "kube-proxy", "eks-pod-identity-agent"]
  }}
```

(Insert before the closing brace; ensure the object stays valid JSON — the preceding `ecr_repository_urls` entry already ends with `}}`, so add the leading comma shown.)

- [ ] **Step 2: Write the fake `terraform` stub**

Create `deploy/k8s/tests/fake-cli/terraform` and `chmod +x` it. It logs, satisfies `output -raw cluster_name` (used by reap in Task 3) and `output -json`, and no-ops `apply`/`destroy`:

```bash
#!/usr/bin/env bash
# Fake terraform for offline tests. Logs the call; apply/destroy succeed;
# output serves the fixture so verify/deploy read canned values.
echo "terraform $*" >> "${KA_TEST_LOG:-/dev/null}"
case "$*" in
  *"output -raw cluster_name"*) echo "ka-daily" ;;
  *"output -json"*)             cat "${KA_TF_OUTPUTS_JSON:-/dev/null}" ;;
  *) : ;;
esac
```

- [ ] **Step 3: Write the fake `helm` and `make` stubs**

Create `deploy/k8s/tests/fake-cli/helm` (`chmod +x`):

```bash
#!/usr/bin/env bash
echo "helm $*" >> "${KA_TEST_LOG:-/dev/null}"
```

Create `deploy/k8s/tests/fake-cli/make` (`chmod +x`). It logs, and when asked to build images it invokes the real `build-push.sh` through the fakes so `up` exercises the whole build path:

```bash
#!/usr/bin/env bash
echo "make $*" >> "${KA_TEST_LOG:-/dev/null}"
case "$*" in
  *images*)
    root=$(cd "$(dirname "$0")/../../../.." && pwd)
    IMAGES_OUTPUTS_JSON="${IMAGES_OUTPUTS_JSON:-$root/deploy/k8s/tests/images-fixture-outputs.json}" \
      "$root/deploy/docker/build-push.sh" >/dev/null ;;
  *) : ;;
esac
```

- [ ] **Step 4: Write the failing test**

Create `deploy/k8s/tests/day_test.sh` and `chmod +x` it. It asserts the cross-layer ordering via the shared log:

```bash
#!/usr/bin/env bash
# Drives day.sh up/down through the fake CLIs and asserts ordering:
#   up   -> both `make images` and `terraform apply` ran; apply precedes the
#           first deploy.sh call (aws eks update-kubeconfig); reseed/rollout ran.
#   down -> `deploy.sh down` (kubectl delete ingress) precedes `terraform destroy`.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
export PATH="$here/fake-cli:$PATH"
export KA_STATE_DIR="$(mktemp -d)"
export KA_NO_NOTIFY=1
export DEPLOY_OUTPUTS_JSON="$here/fixture-outputs.json"
export VERIFY_OUTPUTS_JSON="$here/fixture-outputs.json"
export KA_TF_OUTPUTS_JSON="$here/fixture-outputs.json"
export IMAGES_OUTPUTS_JSON="$here/images-fixture-outputs.json"

line() { grep -n -- "$1" "$KA_TEST_LOG" | head -1 | cut -d: -f1; }
fail=0
want() { if grep -q -- "$1" "$KA_TEST_LOG"; then echo "PASS  $2"; else echo "FAIL  $2"; fail=1; fi; }
before() { local a b; a=$(line "$1"); b=$(line "$2"); if [ -n "$a" ] && [ -n "$b" ] && [ "$a" -lt "$b" ]; then echo "PASS  $3"; else echo "FAIL  $3 (a=$a b=$b)"; fail=1; fi; }

# --- up ---
export KA_TEST_LOG="$(mktemp)"
"$root/deploy/k8s/day.sh" up >/dev/null
want "make images"                    "up builds images"
want "terraform .*apply -auto-approve" "up applies the daily stack"
want "buildx build --platform linux/amd64" "up build reached buildx"
before "terraform .*apply" "aws eks update-kubeconfig" "apply precedes deploy (update-kubeconfig)"
before "aws eks update-kubeconfig" "kubectl" "controller/deploy ran after kubeconfig"

# --- down ---
export KA_TEST_LOG="$(mktemp)"
"$root/deploy/k8s/day.sh" down >/dev/null
want "delete ingress"                  "down deletes the ingress"
want "terraform .*destroy -auto-approve" "down destroys the daily stack"
before "delete ingress" "terraform .*destroy" "ALB released before terraform destroy"

[ "$fail" -eq 0 ] || { echo "day_test FAILED"; exit 1; }
echo "day_test passed"
```

- [ ] **Step 5: Run the test to verify it fails**

Run: `deploy/k8s/tests/day_test.sh`
Expected: FAIL — `day.sh` does not exist yet.

- [ ] **Step 6: Write `day.sh` (up + down)**

Create `deploy/k8s/day.sh` and `chmod +x` it. (Task 3 appends `reap`/`keep` and extends the `case`.)

```bash
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

up() {
  echo "==> building images ‖ applying the daily stack"
  make -C "$root" images &
  local build_pid=$!
  terraform -chdir="$daily" apply -auto-approve
  wait "$build_pid" || { echo "image build failed" >&2; exit 1; }

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

cmd="${1:-}"
shift || true
case "$cmd" in
  up)   up ;;
  down) down ;;
  *) echo "usage: day.sh [up|down|reap|keep]" >&2; exit 2 ;;
esac
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `deploy/k8s/tests/day_test.sh`
Expected: PASS — all `up` and `down` ordering checks green, `day_test passed`.

Note: `make -C "$root" images` resolves to the fake `make` on PATH during the test (bare `make`), which invokes the real `build-push.sh` through the fake docker/aws. If the test cannot see the fake because `make -C` alters lookup, the fake is still a PATH entry — `make` is resolved by name. Confirm the log shows both `make images` and `buildx build`.

- [ ] **Step 8: Wire `day_test.sh` into `check.sh`**

In `deploy/k8s/check.sh`, add after the images test line:

```bash
echo "==> day orchestration test"; "$here/tests/day_test.sh"
```

(`day.sh` is already covered by the existing `"$here"/*.sh` globs for `bash -n` and `shellcheck`.)

- [ ] **Step 9: Run the full offline gate**

Run: `make k8s-check`
Expected: PASS through `day orchestration test`; `all k8s checks passed`.

- [ ] **Step 10: Commit**

```bash
git add deploy/k8s/day.sh deploy/k8s/tests/day_test.sh \
  deploy/k8s/tests/fake-cli/terraform deploy/k8s/tests/fake-cli/helm \
  deploy/k8s/tests/fake-cli/make deploy/k8s/tests/fixture-outputs.json deploy/k8s/check.sh
git commit -m "Add day.sh up/down: sequence the daily stack across layers"
```

---

## Task 3: Forgotten-teardown reaper (`day.sh reap|keep` + LaunchAgent)

**Files:**
- Modify: `deploy/k8s/day.sh` (add `reap`, `keep`, `notify`; extend the `case`)
- Create: `deploy/k8s/reaper/com.ka.daily-reaper.plist.tmpl`
- Create: `deploy/k8s/reaper/install.sh`
- Create: `deploy/k8s/reaper/uninstall.sh`
- Create: `deploy/k8s/tests/reaper_test.sh`
- Modify: `deploy/k8s/check.sh` (cover `reaper/*.sh` in `bash -n`/`shellcheck`; run `reaper_test.sh`)

**Interfaces:**
- Consumes: `day.sh down` (Task 2); the shared `fake-cli/aws` honoring `KA_FAKE_CLUSTER` (Task 1); `KA_STATE_DIR`, `KA_NO_NOTIFY` (Global Constraints).
- Produces: `day.sh reap` — skip if a keep-flag for today exists (log + notify "kept"); else resolve the daily cluster name (`KA_CLUSTER_NAME` override, else `terraform -chdir=<daily> output -raw cluster_name`); if `aws eks describe-cluster` finds it, `notify` + run `down`; if not found or no name, log "nothing to reap". `day.sh keep` — write `$KA_STATE_DIR/keep-<today>` and print confirmation. `notify(msg)` — append to `$state_dir/reaper.log`; unless `KA_NO_NOTIFY`, use `terminal-notifier`/`osascript` if present.
- Produces: `deploy/k8s/reaper/install.sh` renders the plist (substituting the absolute `day.sh` path and `$HOME`) to `~/Library/LaunchAgents/com.ka.daily-reaper.plist` and `launchctl load`s it; `uninstall.sh` unloads + removes it.

- [ ] **Step 1: Write the failing test**

Create `deploy/k8s/tests/reaper_test.sh` and `chmod +x` it:

```bash
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `deploy/k8s/tests/reaper_test.sh`
Expected: FAIL — `day.sh reap` is unknown (falls through to the usage branch, exit 2) and the plist template does not exist.

- [ ] **Step 3: Add `notify`, `reap`, `keep` to `day.sh`**

In `deploy/k8s/day.sh`, add these functions above the trailing `case` (after `down`):

```bash
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
```

Then extend the `case` to dispatch the new subcommands:

```bash
  up)   up ;;
  down) down ;;
  reap) reap ;;
  keep) keep ;;
  *) echo "usage: day.sh [up|down|reap|keep]" >&2; exit 2 ;;
```

- [ ] **Step 4: Write the LaunchAgent plist template**

Create `deploy/k8s/reaper/com.ka.daily-reaper.plist.tmpl`. `@DAYSH@` and `@HOME@` are substituted by `install.sh`. `StartCalendarInterval` fires at 22:00 local wall-clock (tracks CDT/CST automatically):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.ka.daily-reaper</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/bash</string>
    <string>-lc</string>
    <string>@DAYSH@ reap</string>
  </array>
  <key>StartCalendarInterval</key>
  <dict>
    <key>Hour</key><integer>22</integer>
    <key>Minute</key><integer>0</integer>
  </dict>
  <key>StandardOutPath</key><string>@HOME@/.ka/reaper.log</string>
  <key>StandardErrorPath</key><string>@HOME@/.ka/reaper.log</string>
  <key>RunAtLoad</key><false/>
</dict>
</plist>
```

Note: `-lc` runs `day.sh reap` through a login shell so the owner's profile (AWS creds, PATH) is sourced — LaunchAgents otherwise have a minimal environment. This is the verify-on-hardware step called out in the spec.

- [ ] **Step 5: Write `install.sh` and `uninstall.sh`**

Create `deploy/k8s/reaper/install.sh` (`chmod +x`):

```bash
#!/usr/bin/env bash
# Render the LaunchAgent plist with absolute paths and load it. The agent runs
# `day.sh reap` at 22:00 local time daily (see the plist template).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
daysh=$(cd "$here/.." && pwd)/day.sh
agents="$HOME/Library/LaunchAgents"
plist="$agents/com.ka.daily-reaper.plist"
mkdir -p "$agents" "$HOME/.ka"
sed -e "s|@DAYSH@|$daysh|g" -e "s|@HOME@|$HOME|g" \
  "$here/com.ka.daily-reaper.plist.tmpl" > "$plist"
launchctl unload "$plist" >/dev/null 2>&1 || true
launchctl load "$plist"
echo "installed reaper: $plist (fires 22:00 local, runs '$daysh reap')"
```

Create `deploy/k8s/reaper/uninstall.sh` (`chmod +x`):

```bash
#!/usr/bin/env bash
# Unload and remove the reaper LaunchAgent.
set -euo pipefail
plist="$HOME/Library/LaunchAgents/com.ka.daily-reaper.plist"
launchctl unload "$plist" >/dev/null 2>&1 || true
rm -f "$plist"
echo "removed reaper LaunchAgent."
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `deploy/k8s/tests/reaper_test.sh`
Expected: PASS — all five checks green (or `skip plutil`), `reaper_test passed`.

- [ ] **Step 7: Wire the reaper scripts + test into `check.sh`**

In `deploy/k8s/check.sh`, extend the `bash -n` loop and `shellcheck` line to cover `reaper/*.sh`, and run the test.

`bash -n` loop:

```bash
for s in "$here"/*.sh "$here"/reaper/*.sh "$here"/tests/*.sh "$here"/tests/fake-cli/* "$here"/../docker/build-push.sh; do bash -n "$s"; done
```

`shellcheck` line:

```bash
  shellcheck "$here"/*.sh "$here"/reaper/*.sh "$here"/tests/*.sh "$here"/tests/fake-cli/* "$here"/../docker/build-push.sh && echo "PASS shellcheck"
```

Add after the day orchestration test line:

```bash
echo "==> reaper decision test"; "$here/tests/reaper_test.sh"
```

- [ ] **Step 8: Run the full offline gate**

Run: `make k8s-check`
Expected: PASS through `reaper decision test`; `all k8s checks passed`.

- [ ] **Step 9: Commit**

```bash
git add deploy/k8s/day.sh deploy/k8s/reaper/ deploy/k8s/tests/reaper_test.sh deploy/k8s/check.sh
git commit -m "Add the forgotten-teardown reaper: day.sh reap/keep + LaunchAgent"
```

---

## Task 4: Runbook & gotchas documentation

**Files:**
- Modify: `deploy/k8s/README.md` (add a "Daily lifecycle (4c)" section) — create it if absent
- Modify: `CLAUDE.md` (add 4c commands + gotchas)

**Interfaces:**
- Consumes: everything from Tasks 1–3.
- Produces: no code; documentation only. Gate is that the offline suite still passes and the docs match the shipped commands.

- [ ] **Step 1: Document the daily lifecycle**

In `deploy/k8s/README.md`, add a section covering the real commands (adjust to the file's existing style; create the file with a top-level heading if it does not exist):

```markdown
## Daily lifecycle (4c)

One command per bookend, run from the repo root:

- `deploy/k8s/day.sh up` — builds+pushes images (`make images`, linux/amd64) in
  parallel with `terraform -chdir=terraform/daily apply`, runs `verify.sh`, then
  `deploy.sh up latest`, and prints the ALB URL.
- `deploy/k8s/day.sh down` — `deploy.sh down` (releases the ALB) then
  `terraform -chdir=terraform/daily destroy`. Both terraform calls use
  `-auto-approve`; only the disposable daily stack is ever touched.

Forgotten-teardown safety net (macOS):

- `deploy/k8s/reaper/install.sh` installs a LaunchAgent that runs
  `day.sh reap` at 22:00 local time; `uninstall.sh` removes it.
- `day.sh reap` tears the stack down if it is still live and no keep-flag is set.
- `day.sh keep` sets `~/.ka/keep-<today>` so a late demo survives tonight's reaper.
- Logs and flags live under `~/.ka/` (`reaper.log`, `keep-*`).

Requirements: foundation applied (ECR + `aws_region`), Docker with buildx (QEMU
for amd64), AWS creds, `helm`, `kubectl`, `jq`. First reaper run is a
verify-on-hardware step — LaunchAgents have a minimal environment, so confirm the
scheduled `day.sh reap` picks up your AWS profile/PATH (the plist runs it via a
login shell).

Offline validation (no AWS, no cluster): `make k8s-check`.
```

- [ ] **Step 2: Add 4c commands and gotchas to `CLAUDE.md`**

In `CLAUDE.md`, under `## Commands`, add a bullet:

```markdown
- Daily lifecycle (4c): `deploy/k8s/day.sh up` builds+pushes images ‖ `terraform apply`, verifies, deploys, prints the ALB URL; `deploy/k8s/day.sh down` releases the ALB then destroys the daily stack. `make images` builds all three images for `linux/amd64` and pushes to foundation's ECR. `deploy/k8s/reaper/install.sh` installs the 22:00-local auto-teardown LaunchAgent; `day.sh keep` skips it for a late demo.
```

Under `## Gotchas`, add:

```markdown
- 4c terraform paths (`day.sh up`/`down`/`reap`) use `-auto-approve` and target only `terraform -chdir=terraform/daily`; foundation is never applied/destroyed. Never point a 4c script at foundation.
- `make images` reads the ECR registry from the **foundation** stack (`aws_region`, `ecr_repository_urls`), not daily — builds run while the daily stack is mid-apply. Images are `linux/amd64` (nodes are amd64; the laptop is arm64), built via `docker buildx` under emulation, hidden behind OpenSearch creation.
- The reaper LaunchAgent runs `day.sh reap` via a login shell (`bash -lc`) so it inherits AWS creds/PATH; a minimal launchd environment is why a fresh install must be verified on hardware. Keep-flags and logs live under `~/.ka/`.
```

- [ ] **Step 3: Run the offline gate one final time**

Run: `make k8s-check`
Expected: `all k8s checks passed`.

- [ ] **Step 4: Commit**

```bash
git add deploy/k8s/README.md CLAUDE.md
git commit -m "Document the 4c daily lifecycle: runbook and gotchas"
```

---

## Self-Review

**Spec coverage:**
- Image build/push, cross-arch, foundation registry, `latest`, `make images` → Task 1. ✔
- `day.sh up` (parallel build+apply, verify, deploy) and `down` (ordering) → Task 2. ✔
- Reaper: live detection, auto-teardown, keep-flag, 22:00-local LaunchAgent, notify/log, install/uninstall → Task 3. ✔
- Offline gate (`make k8s-check`) extended with fake-CLI sequence/decision/build tests → Tasks 1–3. ✔
- `-auto-approve` safety, daily-only teardown, LaunchAgent-env caveat → encoded in scripts + Task 4 docs. ✔
- Non-goals (DNS/TLS, no manifest/Terraform/Go changes, no CI, single-arch) → nothing in the plan violates them. ✔

**Placeholder scan:** No TBD/TODO; every code and test step is full content. The plist `@DAYSH@`/`@HOME@` are intentional substitution markers, resolved by `install.sh` and the test's `sed`.

**Type/name consistency:** `day.sh` subcommands (`up`/`down`/`reap`/`keep`) and functions match across Tasks 2–3 and the tests; `KA_STATE_DIR`, `KA_TEST_LOG`, `KA_FAKE_CLUSTER`, `KA_NO_NOTIFY`, `KA_CLUSTER_NAME`, `IMAGES_OUTPUTS_JSON`, `KA_TF_OUTPUTS_JSON` are used consistently; foundation output names (`aws_region`, `ecr_repository_urls`) match the confirmed `terraform/foundation/outputs.tf`; the ECR map keys match `deploy.sh`/4b. The shared `fake-cli/aws` and `fake-cli/docker` introduced in Task 1 are reused unchanged in Tasks 2–3.
