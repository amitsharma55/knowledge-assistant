# Images & Orchestration (Sub-project 4c)

Date: 2026-09-13
Status: Approved for planning

## Problem

Sub-projects 4a and 4b are complete. 4a stands up the disposable daily
infrastructure (EKS cluster, single-node OpenSearch, the three Pod Identity
associations) as pure Terraform. 4b makes the app run on that cluster: kustomize
manifests, the ALB controller (Helm), a reseed Job that rebuilds the index from
S3, and an internet-facing HTTP Ingress — all driven by `deploy/k8s/deploy.sh
up|down`, which reads every value from the daily stack's `terraform output`.

Two gaps remain before the owner can run a real demo day:

1. **No images in ECR.** `deploy.sh up` references `knowledge-assistant/{chat-api,
   ui,ingestion}` by tag, but nothing builds and pushes them. The three
   Dockerfiles exist at `deploy/docker/`; nothing invokes them.
2. **No day-level sequencing.** Bringing the app up is a multi-step, cross-layer
   dance (build images, `terraform apply` the daily stack, `verify.sh`,
   `deploy.sh up`), and taking it down has a hard ordering constraint
   (`deploy.sh down` must release the ALB *before* `terraform destroy`, or subnet/
   ENI deletes hang). Doing this by hand every morning and evening is error-prone,
   and a forgotten teardown bills EKS + OpenSearch continuously overnight — which
   defeats the entire reason the stack is disposable.

4c closes both gaps: it builds/pushes the images, wraps the day in a single
`day.sh up|down` orchestrator, and installs a launchd safety net that
auto-tears-down the daily stack at 22:00 (America/Chicago) if the owner forgets.
This is the final phase of sub-project 4.

## Operating model

The owner's daily rhythm becomes two commands:

- **Morning:** `deploy/k8s/day.sh up` — builds and pushes fresh images while the
  daily stack applies in parallel, verifies the infrastructure, deploys the app,
  and prints the ALB URL.
- **Evening:** `deploy/k8s/day.sh down` — releases the ALB and destroys the daily
  stack.

If the evening command is forgotten, a macOS LaunchAgent fires at 22:00 Central
and runs the teardown automatically — unless the owner has flagged "keep tonight"
for a late demo.

`terraform apply` and `terraform destroy` run with `-auto-approve` in all three
paths (`up`, `down`, and the reaper). This is deliberate and safe: the daily
stack is *designed* to be created and destroyed every day, the document corpus
lives in the foundation's S3 bucket (untouched by any 4c command), and the reaper
runs unattended so it cannot prompt. Every teardown targets only
`terraform -chdir=terraform/daily`; foundation (S3, IAM, ECR, secrets) is never
touched.

Images are rebuilt on **every** bring-up (an owner decision: always-fresh, one
mental model). This is affordable because the build overlaps `terraform apply`:
the daily stack's wall-clock is dominated by ~15–20 min of OpenSearch domain
creation, and the cross-arch image build finishes well within that window, so it
adds almost nothing to morning wall-clock. Images are tagged `latest`; because
each morning's EKS cluster is brand-new, its nodes hold no cached layers and pull
fresh from ECR, so the usual "`latest` is stale" hazard does not apply here.

## Goals

- A `make images` target (thin wrapper over `deploy/docker/build-push.sh`) that
  builds all three images for `linux/amd64` and pushes them to the foundation's
  ECR repositories, tagged `latest`.
- A `deploy/k8s/day.sh` orchestrator with `up`, `down`, `reap`, and `keep`
  subcommands that sequences the full daily lifecycle across the 4a (Terraform)
  and 4b (`deploy.sh`) layers, with the ALB-before-`destroy` ordering enforced.
- A macOS LaunchAgent, installed by `deploy/k8s/reaper/install.sh`, that runs
  `day.sh reap` at 22:00 America/Chicago and auto-tears-down a still-running daily
  stack, honoring a per-day "keep" override and always logging + notifying.
- Offline validation with no credentials and no cluster, folded into the existing
  `make k8s-check`: `bash -n` + `shellcheck` on the new scripts, a fake-CLI
  sequence test for `day.sh`, a decision test for the reaper, and a build test for
  `build-push.sh`.

## Non-Goals

- **DNS and TLS/HTTPS.** The demo remains HTTP on the ALB's generated
  `*.elb.amazonaws.com` hostname, exactly as 4b ships it. A real domain, ACM
  certificate, and HTTPS listener stay a documented future phase.
- **Any change to the 4b manifests, the reseed Job, or `deploy.sh`.** 4c calls
  `deploy.sh up`/`down` as-is; it does not modify the workloads or the render.
- **Any change to Terraform stacks or new IAM.** 4c consumes foundation and daily
  outputs; it adds no `.tf`, no roles, and no provider.
- **Any Go changes.** 4c is scripts, a Makefile target, and a plist. No service
  code is touched.
- **CI/CD or a remote/always-on scheduler.** The build runs on the owner's laptop
  and the reaper is a local LaunchAgent. A cloud-side guard (EventBridge/Lambda)
  is out of scope.
- **Multi-arch image manifests.** Only `linux/amd64` is built (the node
  architecture); no arm64 image or multi-platform manifest list.
- **Host cross-compilation of the Go binaries.** The Dockerfiles build inside the
  container under emulation; a host `GOOS=linux GOARCH=amd64` fast path is noted
  as a possible future speedup, not built now.

## Decisions

**Images are built for `linux/amd64` with `docker buildx`, hidden behind
`terraform apply`.** The owner's laptop is arm64 (Apple Silicon); the daily
node group is `t3.medium` (amd64). A naive `docker build` would produce arm64
images the nodes cannot run. `build-push.sh` therefore uses
`docker buildx build --platform linux/amd64 --push` per service, ensuring a
buildx builder exists first. The Go build stages run under amd64 emulation, which
is slow — but `day.sh up` launches the build concurrently with `terraform apply`,
whose ~15–20 min OpenSearch creation dominates wall-clock, so the emulation cost
is effectively free. Host cross-compilation (skip emulation) was considered and
deferred as an optimization the current timing does not need.

**`build-push.sh` reads the registry from *foundation*, not the daily stack.**
The build runs in parallel with `terraform apply` on the daily stack, so the
daily outputs are mid-flux and unreliable at that moment. ECR repositories are
persistent foundation infrastructure, so `build-push.sh` reads
`ecr_repository_urls` and `region` from
`terraform -chdir=terraform/foundation output -json`. An `IMAGES_OUTPUTS_JSON`
environment override (mirroring `deploy.sh`'s `DEPLOY_OUTPUTS_JSON` and
`verify.sh`'s `VERIFY_OUTPUTS_JSON`) lets the offline test drive it with a fixture
and no AWS. ECR auth is `aws ecr get-login-password | docker login`.

**`make images` is a thin wrapper; the logic lives in a script.** Consistent with
the repo convention (`tf-backend`, `deploy.sh`, `verify.sh`): the Makefile recipe
stays trivial and the real work sits in `deploy/docker/build-push.sh`, which is
`shellcheck`-able and testable in isolation. `day.sh up` invokes the build
through `make images` so there is exactly one build entrypoint.

**One orchestrator script with `-auto-approve` everywhere.** `day.sh` sequences
the day so the owner runs one command per bookend. `up` runs `make images` and
`terraform -chdir=terraform/daily apply -auto-approve` concurrently, waits for
both (checking both exit codes), then `verify.sh`, then `deploy.sh up latest`,
then prints the ALB URL. `down` runs `deploy.sh down` (the 4b primitive that
releases the ALB and its ENIs) and *then*
`terraform -chdir=terraform/daily destroy -auto-approve`. Auto-approve is safe
and required here (see Operating model). A Make-target orchestrator was rejected:
backgrounding the parallel build and checking two exit codes is far cleaner in a
script than in a Make recipe. A manual runbook was rejected: the cross-layer
ordering and the parallelism are exactly what is error-prone by hand.

**The reaper detects "still up" with a live AWS check and destroys, not just
alerts.** `day.sh reap` calls `aws eks describe-cluster` on the daily cluster
name; if the cluster exists (the stack is live) and no keep-flag is set for
today, it runs `day.sh down`. Live description is authoritative about what is
actually billing, unlike local Terraform state, which can drift. Auto-destroy
(not alert-only) was the owner's explicit choice: the economic point of the daily
stack is to bill near zero overnight, and relying on the owner noticing a
notification defeats that. The blast radius is bounded — it only ever destroys
the daily stack, and `day.sh down` is idempotent.

**A per-day "keep" flag is the late-demo override.** `day.sh keep` writes
`~/.ka/keep-<YYYY-MM-DD>`; the reaper skips teardown (and notifies "kept") when a
keep-flag matching today's date is present. This lets the owner run a late demo
past 22:00 without the reaper killing it, while defaulting to "reap" so a
forgotten stack is still caught the next night. `day.sh down` clears any keep
flag.

**The reaper runs via a macOS LaunchAgent on local wall-clock.** A LaunchAgent
plist with `StartCalendarInterval` Hour=22 Minute=0 fires at 22:00 in the
machine's local timezone, which tracks the CDT/CST shift automatically — no fixed
UTC offset to update twice a year. `deploy/k8s/reaper/install.sh` renders the
plist template to `~/Library/LaunchAgents/com.ka.daily-reaper.plist` and
`launchctl load`s it; `uninstall.sh` unloads and removes it. A cron entry was
rejected in favor of launchd, the platform-native scheduler on macOS.

**LaunchAgent environment is handled explicitly.** LaunchAgents run with a
minimal environment: no interactive-shell AWS credential setup and a sparse PATH.
`day.sh reap` therefore resolves `AWS_PROFILE`/`AWS_REGION` explicitly and sources
the owner's login profile so the scheduled `aws`/`terraform` calls authenticate
and find their tools. This is the piece most likely to need a small adjustment on
first real run; it is called out in the runbook as a verify-on-hardware step.

## Component layout

```
deploy/docker/
  build-push.sh          buildx linux/amd64 -> ECR for all three services;
                         reads foundation ecr_repository_urls + region
                         (IMAGES_OUTPUTS_JSON override for offline tests)
deploy/k8s/
  day.sh                 orchestrator: up | down | reap | keep
  reaper/
    install.sh           render plist -> ~/Library/LaunchAgents, launchctl load
    uninstall.sh         launchctl unload + remove plist
    com.ka.daily-reaper.plist.tmpl   StartCalendarInterval 22:00 local
  tests/
    day_test.sh          fake-CLI sequence/order assertions for up & down
    reaper_test.sh       reap decision logic (live/keep) + plutil -lint
    images_test.sh       build-push.sh: ECR login + --platform amd64 --push x3
    fake-cli/            + terraform, aws, docker, helm, make stubs (append to log)
Makefile                 + images target -> deploy/docker/build-push.sh
```

`day.sh`, `build-push.sh`, and the reaper scripts sit under `deploy/k8s/` (and
`deploy/docker/`) so the existing `check.sh` globs already `bash -n` and
`shellcheck` them; the reaper scripts under `deploy/k8s/reaper/` are added to
those globs.

## Daily lifecycle flow

`deploy/k8s/day.sh <cmd>`:

1. **up:**
   - Preflight: required tools present (`docker` with buildx, `terraform`, `aws`,
     `helm`, `kubectl`, `jq`); foundation outputs reachable.
   - Launch `make images` in the background **and** `terraform -chdir=terraform/daily
     apply -auto-approve` in the foreground.
   - `wait` for the background build; fail if either the apply or the build
     returned non-zero.
   - `terraform/daily/verify.sh` — cluster ACTIVE, add-ons present, nodes Ready,
     OpenSearch Active.
   - `deploy/k8s/deploy.sh up latest` — controller, workloads, reseed, ALB URL.
   - Print the ALB URL.
2. **down:**
   - `deploy/k8s/deploy.sh down` — delete the Ingress (releasing the ALB and its
     ENIs) and the workloads; uninstall the controller.
   - `terraform -chdir=terraform/daily destroy -auto-approve`.
   - Clear any `~/.ka/keep-*` flag.
3. **reap:** (invoked by the LaunchAgent at 22:00, runnable by hand anytime)
   - If `~/.ka/keep-<today>` exists: log + notify "kept", exit 0.
   - `aws eks describe-cluster` on the daily cluster name. If not found: log
     "nothing to reap", exit 0.
   - Otherwise: log + notify "reaping", run `day.sh down`.
   - Always append to `~/.ka/reaper.log`; notify via `osascript` (and
     `terminal-notifier`/`say` if available).
4. **keep:** write `~/.ka/keep-<today>` so tonight's reaper skips teardown; print
   confirmation.

## Testing & validation

All offline, no credentials, no cluster — folded into `make k8s-check`:

- **`bash -n` + `shellcheck`** on `day.sh`, `build-push.sh`, and the reaper
  scripts (via `check.sh`'s existing globs, extended to `deploy/k8s/reaper/` and
  `deploy/docker/build-push.sh`).
- **`day_test.sh`** — runs `day.sh up` and `day.sh down` with a fake-CLI PATH shim
  whose stubs append to a log, then asserts ordering: for `up`, both `make images`
  and `terraform apply` ran and `verify` preceded `deploy up`; for `down`,
  `deploy.sh down` ran *before* `terraform destroy`.
- **`reaper_test.sh`** — fake `aws eks describe-cluster` returning a live cluster
  ⇒ `day.sh down` is invoked; a keep-flag for today ⇒ teardown is skipped;
  `describe-cluster` not-found ⇒ nothing happens. `plutil -lint` the rendered
  plist when `plutil` is available.
- **`images_test.sh`** — `build-push.sh` with fake `docker`/`aws` and an
  `IMAGES_OUTPUTS_JSON` fixture asserts an ECR `docker login` and a
  `--platform linux/amd64 --push` build for each of the three repositories.
- **New fake-CLI stubs** — `terraform`, `aws`, `docker`, `helm`, `make` added
  under `deploy/k8s/tests/fake-cli/`, each logging its invocation so the sequence
  tests can assert order.

Live paths (`day.sh up`/`down`/`reap` against real AWS, and the installed
LaunchAgent) are owner-run with credentials and are **not** part of `k8s-check`;
they are exercised on real hardware and documented in the runbook, including the
LaunchAgent-environment verify step.

## Dependencies & risks

- **Foundation must be applied** (ECR repositories + `region` output) before
  `make images` runs; offline validation does not need it.
- **buildx + emulation.** `build-push.sh` depends on a working
  `docker buildx`/QEMU setup for `linux/amd64` on arm64. The build is slow under
  emulation but overlaps `terraform apply`; if buildx is misconfigured the
  preflight surfaces it before the apply starts.
- **LaunchAgent environment** (credentials, PATH) is the most likely first-run
  friction point; the design resolves creds/PATH explicitly and the runbook flags
  it as a verify-on-hardware step.
- **Auto-approve destroy** is intentional; the guardrail is that every teardown
  path targets only `terraform -chdir=terraform/daily`, never foundation, and the
  corpus survives in S3 regardless.
