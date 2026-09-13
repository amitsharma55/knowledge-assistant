# Application Deploy (Sub-project 4b) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deploy the knowledge-assistant app onto the 4a EKS cluster each morning — chat-api, UI, the AWS Load Balancer Controller, an index rebuilt from the S3 corpus, and an internet-facing HTTP ALB — all rendered from the daily stack's `terraform output` and applied by a single `deploy.sh`, with offline validation and a post-deploy smoke check.

**Architecture:** Plain Kubernetes manifests under `deploy/k8s/base/` (namespace `knowledge-assistant`, chat-api + UI Deployments/Services, ServiceAccounts, an internet-facing Ingress that fronts the UI, and a one-shot reseed Job). `deploy.sh` reads `terraform -chdir=terraform/daily output -json`, writes a generated kustomize overlay (image refs + tag, a `ka-config` ConfigMap), `helm upgrade --install`s the ALB controller into the service account 4a pre-bound, `kubectl apply -k`s the overlay, runs the reseed Job, and prints the ALB URL. No Kubernetes or Helm provider enters Terraform. Two small 4a touch-ups (namespace tfvars + three pass-through outputs) let `deploy.sh` read a single stack.

**Tech Stack:** Kubernetes manifests + kustomize (via `kubectl kustomize`, kubectl v1.35), bash + jq, Helm (ALB controller, `eks/aws-load-balancer-controller`), AWS CLI v2, Terraform 1.16.2 (only the 4a touch-ups). Optional stronger offline checks: `shellcheck`, `kubeconform` (brew).

**Spec:** `docs/superpowers/specs/2026-09-13-application-deploy-4b-design.md`

## Global Constraints

- **Namespace** `knowledge-assistant` for chat-api, ui, and the reseed Job. The ALB controller lives in `kube-system`.
- **Fixed ServiceAccount names** (must match 4a's Pod Identity associations exactly): `chat-api` and `ingestion` in `knowledge-assistant`; `aws-load-balancer-controller` in `kube-system`. Pod Identity needs **no** IRSA annotation on these SAs.
- **No Kubernetes or Helm provider in Terraform.** Workloads/Helm are applied by `deploy.sh`, never by the daily stack.
- **Container images:** chat-api and ingestion are `gcr.io/distroless/static-debian12` — **no shell**; their entrypoints are the Go binaries. ui is `nginx:1.27-alpine`. The reseed runs the ingestion binary via `args`, once per team, as sequential Pod containers.
- **ECR map keys** (foundation `ecr_repository_urls`, keyed by full repo name): `knowledge-assistant/chat-api`, `knowledge-assistant/ingestion`, `knowledge-assistant/ui`.
- **Bedrock env** (chat-api and reseed embed must use one model so indexed and query vectors match): `KA_EMBED_MODE=bedrock`, `KA_EMBED_MODEL=amazon.titan-embed-text-v2:0`, `KA_EMBED_DIM=1024`. chat-api additionally: `KA_LLM_MODE=anthropic`, `KA_ANTHROPIC_SECRET_ID=ka/anthropic-api-key`, `KA_RERANK_MODE=bedrock`, `KA_RERANK_MODEL=openai.gpt-oss-20b-1:0`, `KA_REWRITE_MODE=bedrock`, `KA_REWRITE_MODEL=openai.gpt-oss-20b-1:0`, `AWS_REGION` from the region output.
- **`KA_RELEVANCE_FLOOR`** compiles to `0.81` (calibrated for local nomic-768). It is embedder-specific; on Bedrock/Titan-v2 it is **provisional** and must be recalibrated with `/check-retrieval` against the running stack. Set it explicitly in the chat-api manifest with a comment saying so — do not silently inherit a value calibrated for a different embedder.
- **`ka-config` ConfigMap** carries two non-sensitive keys: `opensearch_url` (= `https://` + the `opensearch_endpoint` output) and `docs_bucket` (foundation's corpus bucket). No secrets in any manifest.
- **HTTP only.** The Ingress is `internet-facing`, HTTP:80, single default backend → the `ui` Service:80. No host, no TLS, no Route53. The controller auto-discovers foundation's public subnets (already tagged `kubernetes.io/role/elb=1`).
- **Tooling present now:** `kubectl` v1.35 (`kubectl kustomize`, `kubectl apply --dry-run=client`), `jq`, `bash`. Every task gate uses only these. `shellcheck`/`kubeconform` run **if installed** (stronger, optional); `helm` is required only for a live `deploy.sh up`.
- **chat-api routes:** `GET /healthz` → `200 "ok"`; `GET /v1/teams` is unauthenticated; `POST /v1/chat/messages` needs `X-Team` + `X-Dev-Groups`.

---

## Task 1: 4a touch-ups — pass-through outputs and app namespace

**Files:**
- Modify: `terraform/daily/outputs.tf` (add three outputs)
- Modify: `terraform/daily/terraform.tfvars` (two namespace values)
- Modify: `terraform/daily/tests/daily.tftest.hcl` (update namespace expectations; assert new outputs)

**Interfaces:**
- Produces (the 4a → 4b contract `deploy.sh` reads): outputs `docs_bucket` (string), `ecr_repository_urls` (map(string), keyed by full repo name), `vpc_id` (string), plus the existing `cluster_name`, `cluster_endpoint`, `cluster_certificate_authority_data`, `region`, `opensearch_endpoint`, `node_group_name`, `verify_expectations`.
- Produces: chat-api and ingestion Pod Identity associations bound in namespace `knowledge-assistant`.

- [ ] **Step 1: Add the failing test expectations**

In `terraform/daily/tests/daily.tftest.hcl`, change the two variable overrides in the `variables { … }` block:

```hcl
  chat_api_namespace      = "knowledge-assistant"
  ingestion_namespace     = "knowledge-assistant"
  lb_controller_namespace = "kube-system"
```

Then, inside `run "pod_identity_bindings" { … }`, add assertions for the new namespace and the three pass-through outputs:

```hcl
  assert {
    condition     = aws_eks_pod_identity_association.chat_api.namespace == "knowledge-assistant"
    error_message = "chat-api association must bind the knowledge-assistant namespace"
  }
  assert {
    condition     = aws_eks_pod_identity_association.ingestion.namespace == "knowledge-assistant"
    error_message = "ingestion association must bind the knowledge-assistant namespace"
  }
  assert {
    condition     = output.docs_bucket != "" && output.vpc_id != ""
    error_message = "docs_bucket and vpc_id must be passed through from foundation"
  }
  assert {
    condition     = output.ecr_repository_urls["knowledge-assistant/chat-api"] != ""
    error_message = "ecr_repository_urls must expose the chat-api repository URL"
  }
```

(If the mocked foundation outputs in the test's setup block do not already define `docs_bucket`, `vpc_id`, and `ecr_repository_urls`, add them there with fake values, e.g. `docs_bucket = "ka-docs-123456789012"`, `vpc_id = "vpc-000"`, and `ecr_repository_urls = { "knowledge-assistant/chat-api" = "123456789012.dkr.ecr.us-east-1.amazonaws.com/knowledge-assistant/chat-api", "knowledge-assistant/ingestion" = "…/ingestion", "knowledge-assistant/ui" = "…/ui" }`.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `terraform -chdir=terraform/daily test`
Expected: FAIL — the namespace assertions fail (associations still bind `default`) and/or the output assertions fail (outputs not defined yet).

- [ ] **Step 3: Add the pass-through outputs**

Append to `terraform/daily/outputs.tf`:

```hcl
output "docs_bucket" {
  description = "Foundation's document corpus bucket; the reseed Job reads the S3 corpus from it."
  value       = local.foundation.docs_bucket
}

output "ecr_repository_urls" {
  description = "Foundation's ECR repository URLs, keyed by repository name; deploy.sh builds image refs from these."
  value       = local.foundation.ecr_repository_urls
}

output "vpc_id" {
  description = "Foundation's VPC id; the ALB controller Helm install needs it."
  value       = local.foundation.vpc_id
}
```

- [ ] **Step 4: Retarget the app namespace in tfvars**

In `terraform/daily/terraform.tfvars`, change:

```hcl
chat_api_namespace      = "knowledge-assistant"
ingestion_namespace     = "knowledge-assistant"
lb_controller_namespace = "kube-system"
```

- [ ] **Step 5: Run the full offline check**

Run: `make tf-check`
Expected: PASS — fmt clean, validate passes, `terraform test` passes (all daily + foundation runs), literals guard passes. `terraform test` should report the daily suite green with the new assertions.

- [ ] **Step 6: Commit**

```bash
git add terraform/daily/outputs.tf terraform/daily/terraform.tfvars terraform/daily/tests/daily.tftest.hcl
git commit -m "Pass docs bucket, ECR URLs and VPC id through the daily stack, and run the app in the knowledge-assistant namespace

4b's deploy.sh reads only the daily stack's outputs (as verify.sh does), so
re-expose the three foundation values it needs. Move the chat-api and
ingestion pods to a dedicated namespace instead of default."
```

---

## Task 2: kustomize base — namespace, service accounts, chat-api, UI

**Files:**
- Create: `deploy/k8s/base/namespace.yaml`
- Create: `deploy/k8s/base/serviceaccounts.yaml`
- Create: `deploy/k8s/base/chat-api.yaml`
- Create: `deploy/k8s/base/ui.yaml`
- Create: `deploy/k8s/base/kustomization.yaml`
- Delete: `deploy/k8s/namespace.yaml`, `deploy/k8s/chat-api.yaml`, `deploy/k8s/ui.yaml`, `deploy/k8s/ingestion-cron.yaml` (superseded sketches)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces (later tasks and the overlay rely on these): kustomize **image names** `ka/chat-api`, `ka/ui` (the overlay's `images:` transformer rewrites these); ConfigMap name `ka-config` with keys `opensearch_url`, `docs_bucket` (generated by the overlay in Task 4); Service names `chat-api` (:8080) and `ui` (:80) in namespace `knowledge-assistant`.

- [ ] **Step 1: Write the namespace and service accounts**

`deploy/k8s/base/namespace.yaml`:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: knowledge-assistant
```

`deploy/k8s/base/serviceaccounts.yaml` (annotation-free — 4a's Pod Identity associations bind these by name):

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: chat-api
  namespace: knowledge-assistant
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ingestion
  namespace: knowledge-assistant
```

- [ ] **Step 2: Write the chat-api Deployment + Service**

`deploy/k8s/base/chat-api.yaml`. Image name `ka/chat-api` with a placeholder tag (the overlay rewrites both). Single replica, no HPA (Global Constraints).

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: chat-api
  namespace: knowledge-assistant
spec:
  replicas: 1
  selector:
    matchLabels: { app: chat-api }
  template:
    metadata:
      labels: { app: chat-api }
    spec:
      serviceAccountName: chat-api  # EKS Pod Identity: Bedrock + Secrets Manager + OpenSearch
      containers:
        - name: chat-api
          image: ka/chat-api:placeholder  # overlay rewrites via images: transformer
          ports: [{ containerPort: 8080 }]
          env:
            - { name: KA_LLM_MODE, value: anthropic }
            - { name: AWS_REGION, value: us-east-1 }
            # Must match foundation's anthropic_secret_name.
            - { name: KA_ANTHROPIC_SECRET_ID, value: ka/anthropic-api-key }
            - { name: KA_EMBED_MODE, value: bedrock }
            - { name: KA_EMBED_MODEL, value: "amazon.titan-embed-text-v2:0" }
            - { name: KA_EMBED_DIM, value: "1024" }
            - { name: KA_RERANK_MODE, value: bedrock }
            - { name: KA_RERANK_MODEL, value: "openai.gpt-oss-20b-1:0" }
            - { name: KA_REWRITE_MODE, value: bedrock }
            - { name: KA_REWRITE_MODEL, value: "openai.gpt-oss-20b-1:0" }
            # PROVISIONAL: 0.81 is calibrated for local nomic-768. Recalibrate
            # for Titan-v2 with /check-retrieval against the running stack.
            - { name: KA_RELEVANCE_FLOOR, value: "0.81" }
            - name: KA_OPENSEARCH_URL
              valueFrom: { configMapKeyRef: { name: ka-config, key: opensearch_url } }
          readinessProbe:
            httpGet: { path: /healthz, port: 8080 }
            initialDelaySeconds: 3
          livenessProbe:
            httpGet: { path: /healthz, port: 8080 }
            initialDelaySeconds: 10
          resources:
            requests: { cpu: "250m", memory: "256Mi" }
            limits:   { cpu: "1",    memory: "512Mi" }
---
apiVersion: v1
kind: Service
metadata:
  name: chat-api
  namespace: knowledge-assistant
spec:
  selector: { app: chat-api }
  ports:
    - { port: 8080, targetPort: 8080 }
```

- [ ] **Step 3: Write the UI Deployment + Service**

`deploy/k8s/base/ui.yaml`. nginx serves the static build and proxies `/v1` to `chat-api:8080` (per `deploy/docker/nginx.conf`).

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ui
  namespace: knowledge-assistant
spec:
  replicas: 1
  selector:
    matchLabels: { app: ui }
  template:
    metadata:
      labels: { app: ui }
    spec:
      containers:
        - name: ui
          image: ka/ui:placeholder  # overlay rewrites via images: transformer
          ports: [{ containerPort: 80 }]
          readinessProbe:
            httpGet: { path: /, port: 80 }
            initialDelaySeconds: 3
          resources:
            requests: { cpu: "50m",  memory: "64Mi" }
            limits:   { cpu: "200m", memory: "128Mi" }
---
apiVersion: v1
kind: Service
metadata:
  name: ui
  namespace: knowledge-assistant
spec:
  selector: { app: ui }
  ports:
    - { port: 80, targetPort: 80 }
```

- [ ] **Step 4: Write the base kustomization**

`deploy/k8s/base/kustomization.yaml` (ingress and reseed job are added in Task 3):

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - namespace.yaml
  - serviceaccounts.yaml
  - chat-api.yaml
  - ui.yaml
```

- [ ] **Step 5: Delete the superseded sketch manifests**

```bash
git rm deploy/k8s/namespace.yaml deploy/k8s/chat-api.yaml deploy/k8s/ui.yaml deploy/k8s/ingestion-cron.yaml
```

- [ ] **Step 6: Validate the base renders and is well-formed**

Run: `kubectl kustomize deploy/k8s/base | kubectl apply --dry-run=client -f -`
Expected: PASS — prints `… created (dry run)` for the Namespace, two ServiceAccounts, two Deployments and two Services, with no schema errors.

- [ ] **Step 7: Commit**

```bash
git add deploy/k8s/base
git commit -m "Add kustomize base for the chat-api and UI workloads

Single-replica Deployments in the knowledge-assistant namespace, plain image
names the deploy overlay rewrites, KA_OPENSEARCH_URL from the ka-config
ConfigMap. Replaces the earlier sketch manifests."
```

---

## Task 3: Ingress and the S3 reseed Job

**Files:**
- Create: `deploy/k8s/base/ingress.yaml`
- Create: `deploy/k8s/base/reseed-job.yaml`
- Create: `deploy/k8s/base/reseed-env.yaml`
- Modify: `deploy/k8s/base/kustomization.yaml` (add the three resources)

**Interfaces:**
- Consumes: the `ui` Service (:80) and `ka-config` ConfigMap keys `opensearch_url` and `docs_bucket`; kustomize image name `ka/ingestion` (overlay rewrites it).
- Produces: an `Ingress` named `ka` (internet-facing ALB, default backend → ui:80) whose `.status.loadBalancer.ingress[0].hostname` is the demo URL; a `Job` named `reseed` that completes after indexing coupa, star and hr.

- [ ] **Step 1: Write the Ingress**

`deploy/k8s/base/ingress.yaml`. No host (default backend), HTTP:80, internet-facing; the controller auto-discovers the elb-tagged public subnets.

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ka
  namespace: knowledge-assistant
  annotations:
    alb.ingress.kubernetes.io/scheme: internet-facing
    alb.ingress.kubernetes.io/target-type: ip
    alb.ingress.kubernetes.io/listen-ports: '[{"HTTP":80}]'
    alb.ingress.kubernetes.io/healthcheck-path: /
spec:
  ingressClassName: alb
  defaultBackend:
    service:
      name: ui
      port: { number: 80 }
```

- [ ] **Step 2: Write the reseed Job**

`deploy/k8s/base/reseed-job.yaml`. The distroless ingestion image has no shell, so the three teams run as sequential Pod containers — `coupa` and `star` as `initContainers` (run in order, each to completion), `hr` as the main container. Sequential ordering avoids two invocations racing to create the index on a 404. All three share the `ka/ingestion` image (overlay rewrites it) and the same env; only `-team` differs. `restartPolicy: Never`, small `backoffLimit`.

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: reseed
  namespace: knowledge-assistant
spec:
  backoffLimit: 1
  template:
    spec:
      serviceAccountName: ingestion  # Pod Identity: S3 (read corpus) + Bedrock (embed) + OpenSearch (write)
      restartPolicy: Never
      initContainers:
        - name: reseed-coupa
          image: ka/ingestion:placeholder
          args: ["-source", "s3", "-team", "coupa"]
          envFrom: [{ configMapRef: { name: reseed-env } }]
          env:
            - name: KA_OPENSEARCH_URL
              valueFrom: { configMapKeyRef: { name: ka-config, key: opensearch_url } }
            - name: KA_DOCS_BUCKET
              valueFrom: { configMapKeyRef: { name: ka-config, key: docs_bucket } }
        - name: reseed-star
          image: ka/ingestion:placeholder
          args: ["-source", "s3", "-team", "star"]
          envFrom: [{ configMapRef: { name: reseed-env } }]
          env:
            - name: KA_OPENSEARCH_URL
              valueFrom: { configMapKeyRef: { name: ka-config, key: opensearch_url } }
            - name: KA_DOCS_BUCKET
              valueFrom: { configMapKeyRef: { name: ka-config, key: docs_bucket } }
      containers:
        - name: reseed-hr
          image: ka/ingestion:placeholder
          args: ["-source", "s3", "-team", "hr"]
          envFrom: [{ configMapRef: { name: reseed-env } }]
          env:
            - name: KA_OPENSEARCH_URL
              valueFrom: { configMapKeyRef: { name: ka-config, key: opensearch_url } }
            - name: KA_DOCS_BUCKET
              valueFrom: { configMapKeyRef: { name: ka-config, key: docs_bucket } }
```

`reseed-env` is a second ConfigMap holding the shared Bedrock embed env (so the reseed and chat-api embed with one model). Create it as a plain manifest so it is part of the base — it has no per-environment values.

`deploy/k8s/base/reseed-env.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: reseed-env
  namespace: knowledge-assistant
data:
  AWS_REGION: us-east-1
  KA_EMBED_MODE: bedrock
  KA_EMBED_MODEL: "amazon.titan-embed-text-v2:0"
  KA_EMBED_DIM: "1024"
```

- [ ] **Step 3: Add the new resources to the base kustomization**

Edit `deploy/k8s/base/kustomization.yaml` `resources:` to:

```yaml
resources:
  - namespace.yaml
  - serviceaccounts.yaml
  - reseed-env.yaml
  - chat-api.yaml
  - ui.yaml
  - ingress.yaml
  - reseed-job.yaml
```

- [ ] **Step 4: Validate the full base renders**

Run: `kubectl kustomize deploy/k8s/base | kubectl apply --dry-run=client -f -`
Expected: PASS — the Ingress, the reseed Job and the reseed-env ConfigMap now render alongside the Task 2 resources, with no schema errors. (`ka-config` itself is generated by the overlay in Task 4; the base need not resolve the `configMapKeyRef` for a client dry-run.)

- [ ] **Step 5: Commit**

```bash
git add deploy/k8s/base/ingress.yaml deploy/k8s/base/reseed-job.yaml deploy/k8s/base/reseed-env.yaml deploy/k8s/base/kustomization.yaml
git commit -m "Add the internet-facing Ingress and the S3 reseed Job

The ALB fronts the UI (nginx proxies /v1 to chat-api); the reseed Job rebuilds
the index from S3 for coupa/star/hr as sequential containers, because the
distroless indexer image has no shell and EnsureIndex races on an empty index."
```

---

## Task 4: `deploy.sh render` — generate the overlay offline

**Files:**
- Create: `deploy/k8s/deploy.sh` (the `render` subcommand only in this task; `up`/`down` in Task 5)
- Create: `deploy/k8s/tests/fixture-outputs.json`
- Create: `deploy/k8s/tests/render_test.sh`
- Modify: `.gitignore` (ignore `deploy/k8s/.generated/`)

**Interfaces:**
- Consumes: the daily stack's outputs — either from `terraform -chdir=terraform/daily output -json`, or, when `DEPLOY_OUTPUTS_JSON` names a file, from that file (mirrors `verify.sh`'s `VERIFY_OUTPUTS_JSON`, so rendering is testable with no AWS). Reads `.ecr_repository_urls.value["knowledge-assistant/{chat-api,ui,ingestion}"]`, `.opensearch_endpoint.value`, `.docs_bucket.value`.
- Produces: `deploy/k8s/.generated/kustomization.yaml` — an overlay with `resources: [../base]`, an `images:` transformer rewriting `ka/chat-api`, `ka/ui`, `ka/ingestion` to the ECR repo + tag, and a `configMapGenerator` for `ka-config` (`opensearch_url`, `docs_bucket`; `disableNameSuffixHash: true` so consumers resolve a stable name). Later tasks call `render`, `up`, `down`.

- [ ] **Step 1: Write the render fixture**

`deploy/k8s/tests/fixture-outputs.json` (the `terraform output -json` shape — each output is `{"value": …}`):

```json
{
  "cluster_name": {"value": "ka-daily"},
  "region": {"value": "us-east-1"},
  "vpc_id": {"value": "vpc-000"},
  "opensearch_endpoint": {"value": "vpc-ka-abc.us-east-1.es.amazonaws.com"},
  "docs_bucket": {"value": "ka-docs-123456789012"},
  "ecr_repository_urls": {"value": {
    "knowledge-assistant/chat-api": "123456789012.dkr.ecr.us-east-1.amazonaws.com/knowledge-assistant/chat-api",
    "knowledge-assistant/ui": "123456789012.dkr.ecr.us-east-1.amazonaws.com/knowledge-assistant/ui",
    "knowledge-assistant/ingestion": "123456789012.dkr.ecr.us-east-1.amazonaws.com/knowledge-assistant/ingestion"
  }}
}
```

- [ ] **Step 2: Write the render test (failing)**

`deploy/k8s/tests/render_test.sh`:

```bash
#!/usr/bin/env bash
# Renders the deploy overlay from fixture outputs (no AWS) and asserts the image
# refs and ka-config values landed. Uses kubectl kustomize (present) only.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)

export DEPLOY_OUTPUTS_JSON="$here/fixture-outputs.json"
"$root/deploy/k8s/deploy.sh" render v1.2.3

rendered=$(kubectl kustomize "$root/deploy/k8s/.generated")
fail=0
check() { if grep -q "$1" <<<"$rendered"; then echo "PASS  $2"; else echo "FAIL  $2"; fail=1; fi; }
check "knowledge-assistant/chat-api:v1.2.3"  "chat-api image rewritten with repo + tag"
check "knowledge-assistant/ui:v1.2.3"        "ui image rewritten"
check "knowledge-assistant/ingestion:v1.2.3" "ingestion image rewritten"
check "opensearch_url: https://vpc-ka-abc.us-east-1.es.amazonaws.com" "ka-config opensearch_url set with https scheme"
check "docs_bucket: ka-docs-123456789012"    "ka-config docs_bucket set"
check "name: ka-config"                      "ka-config named stably (no hash suffix)"
[ "$fail" -eq 0 ] || { echo "render_test FAILED"; exit 1; }
echo "render_test passed"
```

```bash
chmod +x deploy/k8s/tests/render_test.sh
```

- [ ] **Step 3: Run it to verify it fails**

Run: `deploy/k8s/tests/render_test.sh`
Expected: FAIL — `deploy.sh` does not exist yet (`No such file or directory`).

- [ ] **Step 4: Write `deploy.sh` with the `render` subcommand**

`deploy/k8s/deploy.sh`:

```bash
#!/usr/bin/env bash
# deploy.sh brings the app up on (or takes it down from) the daily EKS cluster.
# Every environment value comes from the daily stack's `terraform output`, so
# nothing here repeats what tfvars already set. Usage:
#   deploy.sh up   [image-tag]   # helm-install the ALB controller, apply workloads, reseed, print URL
#   deploy.sh down               # release the ALB and delete workloads (run before terraform destroy)
#   deploy.sh render [image-tag] # write the kustomize overlay only (offline; used by tests)
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
gen="$here/.generated"
daily="$here/../../terraform/daily"
# Pin to a chart version tested against the cluster's Kubernetes version; check
# with `helm search repo eks/aws-load-balancer-controller`.
LBC_CHART_VERSION="${LBC_CHART_VERSION:-1.8.1}"

outputs_json() {
  if [ -n "${DEPLOY_OUTPUTS_JSON:-}" ]; then
    cat "$DEPLOY_OUTPUTS_JSON"
  else
    terraform -chdir="$daily" output -json
  fi
}

render() {
  local tag="${1:-latest}"
  local outputs; outputs=$(outputs_json)
  out() { jq -r "$1" <<<"$outputs"; }
  local chat ui ingest os bucket
  chat=$(out '.ecr_repository_urls.value["knowledge-assistant/chat-api"]')
  ui=$(out '.ecr_repository_urls.value["knowledge-assistant/ui"]')
  ingest=$(out '.ecr_repository_urls.value["knowledge-assistant/ingestion"]')
  os=$(out '.opensearch_endpoint.value')
  bucket=$(out '.docs_bucket.value')
  mkdir -p "$gen"
  cat > "$gen/kustomization.yaml" <<YAML
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../base
images:
  - name: ka/chat-api
    newName: $chat
    newTag: $tag
  - name: ka/ui
    newName: $ui
    newTag: $tag
  - name: ka/ingestion
    newName: $ingest
    newTag: $tag
configMapGenerator:
  - name: ka-config
    literals:
      - opensearch_url=https://$os
      - docs_bucket=$bucket
    options:
      disableNameSuffixHash: true
YAML
  echo "rendered overlay to $gen (tag $tag)"
}

cmd="${1:-up}"; shift || true
case "$cmd" in
  render) render "${1:-latest}" ;;
  up|down) echo "up/down implemented in a later step" >&2; exit 3 ;;
  *) echo "usage: deploy.sh [up|down|render] [image-tag]" >&2; exit 2 ;;
esac
```

```bash
chmod +x deploy/k8s/deploy.sh
```

- [ ] **Step 5: Ignore the generated overlay**

Add to `.gitignore`:

```
deploy/k8s/.generated/
```

- [ ] **Step 6: Run the render test to verify it passes**

Run: `deploy/k8s/tests/render_test.sh`
Expected: PASS — all six checks PASS, `render_test passed`.

- [ ] **Step 7: Commit**

```bash
git add deploy/k8s/deploy.sh deploy/k8s/tests/fixture-outputs.json deploy/k8s/tests/render_test.sh .gitignore
git commit -m "Render the deploy overlay from the daily stack's outputs

deploy.sh render writes a kustomize overlay (image refs + tag, the ka-config
ConfigMap) from terraform output, injectable via DEPLOY_OUTPUTS_JSON so it is
testable with no AWS."
```

---

## Task 5: `deploy.sh up` / `down` — live orchestration

**Files:**
- Modify: `deploy/k8s/deploy.sh` (replace the `up|down` stub with real functions)

**Interfaces:**
- Consumes: `render` (Task 4); daily outputs `cluster_name`, `region`, `vpc_id`; `helm`, `aws`, `kubectl` on PATH; a live cluster + credentials.
- Produces: `up` leaves the app running and prints `app: http://<alb-host>/`; `down` releases the ALB and deletes the workloads (the primitive 4c sequences before `terraform destroy`).

- [ ] **Step 1: Replace the stub with `up` and `down`**

In `deploy/k8s/deploy.sh`, add these two functions above the `case`, and replace the `up|down` stub line:

```bash
up() {
  local tag="${1:-latest}"
  local outputs; outputs=$(outputs_json)
  out() { jq -r "$1" <<<"$outputs"; }
  local cluster region vpc
  cluster=$(out '.cluster_name.value'); region=$(out '.region.value'); vpc=$(out '.vpc_id.value')

  aws eks update-kubeconfig --name "$cluster" --region "$region"

  # ALB controller into the kube-system SA 4a pre-bound via Pod Identity (no
  # IRSA annotation needed). Helm here, not Terraform, keeps the daily stack
  # AWS-provider-only and offline-testable.
  helm repo add eks https://aws.github.io/eks-charts >/dev/null 2>&1 || true
  helm repo update eks >/dev/null
  helm upgrade --install aws-load-balancer-controller eks/aws-load-balancer-controller \
    --version "$LBC_CHART_VERSION" \
    --namespace kube-system \
    --set clusterName="$cluster" \
    --set region="$region" \
    --set vpcId="$vpc" \
    --set serviceAccount.create=false \
    --set serviceAccount.name=aws-load-balancer-controller
  kubectl -n kube-system rollout status deploy/aws-load-balancer-controller --timeout=180s

  render "$tag"
  # A Job is immutable once created; on a re-run delete the previous one first.
  kubectl -n knowledge-assistant delete job reseed --ignore-not-found
  kubectl apply -k "$gen"
  kubectl -n knowledge-assistant rollout status deploy/chat-api --timeout=180s
  kubectl -n knowledge-assistant rollout status deploy/ui --timeout=180s
  kubectl -n knowledge-assistant wait --for=condition=complete job/reseed --timeout=600s

  local host=""
  for _ in $(seq 1 60); do
    host=$(kubectl -n knowledge-assistant get ingress ka \
      -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)
    [ -n "$host" ] && break
    sleep 5
  done
  if [ -n "$host" ]; then
    echo "app: http://$host/"
  else
    echo "ingress has no ALB hostname yet; check: kubectl -n knowledge-assistant get ingress ka"
  fi
}

down() {
  # Delete the Ingress FIRST and wait: the controller runs its finalizer, which
  # deletes the real ALB and its ENIs. Skipping this makes 4a's later
  # `terraform destroy` hang on subnet/ENI dependencies. This is the primitive
  # 4c's evening-down sequences before destroying the daily stack.
  kubectl -n knowledge-assistant delete ingress ka --ignore-not-found --wait=true
  kubectl delete namespace knowledge-assistant --ignore-not-found
  helm -n kube-system uninstall aws-load-balancer-controller >/dev/null 2>&1 || true
  echo "Ingress/ALB released and workloads deleted. Safe to 'terraform destroy' the daily stack."
}
```

Replace the case line `up|down) echo … exit 3 ;;` with:

```bash
  up)   up "${1:-latest}" ;;
  down) down ;;
```

- [ ] **Step 2: Verify the script still parses and renders**

Run: `bash -n deploy/k8s/deploy.sh && deploy/k8s/tests/render_test.sh`
Expected: PASS — no syntax errors; `render_test passed` (the `render` path is unchanged).

- [ ] **Step 3: Commit**

```bash
git add deploy/k8s/deploy.sh
git commit -m "Bring the app up and down on the daily cluster

up helm-installs the ALB controller into the pre-bound SA, applies the
workloads, rebuilds the index from S3, and prints the ALB URL. down deletes
the Ingress first (releasing the ALB) so a later terraform destroy does not
hang on ENIs."
```

---

## Task 6: `smoke.sh` — post-deploy live checks

**Files:**
- Create: `deploy/k8s/smoke.sh`
- Create: `deploy/k8s/tests/fake-cli/kubectl`, `deploy/k8s/tests/fake-cli/curl`
- Create: `deploy/k8s/tests/smoke_test.sh`

**Interfaces:**
- Consumes: a deployed app + kubeconfig (live); for the offline test, fake `kubectl`/`curl` on PATH returning healthy values.
- Produces: `smoke.sh` prints PASS/FAIL lines and exits nonzero on any failure; `all smoke checks passed` on success.

- [ ] **Step 1: Write smoke.sh**

`deploy/k8s/smoke.sh`:

```bash
#!/usr/bin/env bash
# smoke.sh checks the deployed app after `deploy.sh up`: the ALB controller,
# chat-api and ui are Ready, the reseed Job completed, the Ingress has an ALB
# hostname, /healthz answers 200, and the unauthenticated /v1/teams answers 200.
# Run with the credentials/kubeconfig deploy.sh set up.
set -euo pipefail
ns=knowledge-assistant
fail=0
pass(){ echo "PASS  $1"; }
failc(){ echo "FAIL  $1"; fail=1; }

ready(){ kubectl -n "$1" get deploy "$2" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true; }
[ "$(ready kube-system aws-load-balancer-controller)" = "1" ] && pass "ALB controller Ready" || failc "ALB controller not Ready"
[ "$(ready "$ns" chat-api)" -ge 1 ] 2>/dev/null && pass "chat-api Ready" || failc "chat-api not Ready"
[ "$(ready "$ns" ui)" -ge 1 ] 2>/dev/null && pass "ui Ready" || failc "ui not Ready"

jobc=$(kubectl -n "$ns" get job reseed -o jsonpath='{.status.succeeded}' 2>/dev/null || true)
[ "$jobc" = "1" ] && pass "reseed Job complete" || failc "reseed Job not complete (succeeded=$jobc)"

host=$(kubectl -n "$ns" get ingress ka -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)
[ -n "$host" ] && pass "Ingress ALB hostname: $host" || failc "Ingress has no ALB hostname"

if [ -n "$host" ]; then
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://$host/healthz" || true)
  [ "$code" = "200" ] && pass "/healthz 200" || failc "/healthz returned $code"
  code=$(curl -s -o /dev/null -w '%{http_code}' -H 'X-Dev-Groups: everyone' "http://$host/v1/teams" || true)
  [ "$code" = "200" ] && pass "/v1/teams 200" || failc "/v1/teams returned $code"
fi

[ "$fail" -eq 0 ] || { echo "$fail smoke check(s) failed"; exit 1; }
echo "all smoke checks passed"
```

```bash
chmod +x deploy/k8s/smoke.sh
```

- [ ] **Step 2: Write the fake CLIs**

`deploy/k8s/tests/fake-cli/kubectl` (branch on the jsonpath in the args):

```bash
#!/usr/bin/env bash
case "$*" in
  *readyReplicas*)          echo 1 ;;
  *status.succeeded*)       echo 1 ;;
  *loadBalancer*hostname*)  echo "ka-alb.us-east-1.elb.amazonaws.com" ;;
  *)                        echo "" ;;
esac
```

`deploy/k8s/tests/fake-cli/curl` (every request looks healthy):

```bash
#!/usr/bin/env bash
echo 200
```

```bash
chmod +x deploy/k8s/tests/fake-cli/kubectl deploy/k8s/tests/fake-cli/curl
```

- [ ] **Step 3: Write the smoke test (failing)**

`deploy/k8s/tests/smoke_test.sh`:

```bash
#!/usr/bin/env bash
# Runs smoke.sh against fake kubectl/curl returning healthy values; asserts it
# reports success. Proves the check logic, not a live cluster.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
export PATH="$here/fake-cli:$PATH"
out=$(mktemp)
if "$root/deploy/k8s/smoke.sh" >"$out" 2>&1 && grep -q "all smoke checks passed" "$out"; then
  echo "smoke_test passed"
else
  echo "smoke_test FAILED:"; cat "$out"; exit 1
fi
```

```bash
chmod +x deploy/k8s/tests/smoke_test.sh
```

- [ ] **Step 4: Run it to verify it passes**

Run: `deploy/k8s/tests/smoke_test.sh`
Expected: PASS — `smoke_test passed`. (If run before smoke.sh existed it would fail with "No such file or directory"; run it now to confirm green.)

- [ ] **Step 5: Commit**

```bash
git add deploy/k8s/smoke.sh deploy/k8s/tests/fake-cli deploy/k8s/tests/smoke_test.sh
git commit -m "Add a post-deploy smoke check with a fake-CLI offline test

smoke.sh verifies the controller, workloads, reseed Job, Ingress ALB and the
/healthz and /v1/teams endpoints; smoke_test.sh proves the logic against fake
kubectl/curl so it runs in make k8s-check without a cluster."
```

---

## Task 7: `make k8s-check` — one offline gate for 4b

**Files:**
- Create: `deploy/k8s/check.sh`
- Modify: `Makefile` (add `k8s-check` to `.PHONY` and a target)

**Interfaces:**
- Consumes: everything from Tasks 2–6; `kubectl`, `jq`, `bash` (required); `shellcheck`, `kubeconform` (optional, run if installed).
- Produces: a single command that fails if any base manifest, overlay render, or smoke logic is broken.

- [ ] **Step 1: Write check.sh**

`deploy/k8s/check.sh`:

```bash
#!/usr/bin/env bash
# Offline validation for the 4b deploy assets. Uses only kubectl + jq + bash;
# additionally runs shellcheck and kubeconform when installed. No AWS, no cluster.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)

echo "==> base manifests render + client-side validate"
kubectl kustomize "$here/base" | kubectl apply --dry-run=client -f - >/dev/null
echo "PASS base"

echo "==> shell syntax (bash -n)"
for s in "$here"/*.sh "$here"/tests/*.sh "$here"/tests/fake-cli/*; do bash -n "$s"; done
echo "PASS bash -n"

echo "==> overlay render test"; "$here/tests/render_test.sh"
echo "==> smoke logic test";   "$here/tests/smoke_test.sh"

if command -v shellcheck >/dev/null 2>&1; then
  echo "==> shellcheck"
  shellcheck "$here"/*.sh "$here"/tests/*.sh && echo "PASS shellcheck"
else
  echo "skip shellcheck (not installed; brew install shellcheck for a stronger check)"
fi

if command -v kubeconform >/dev/null 2>&1; then
  echo "==> kubeconform"
  kubectl kustomize "$here/base" | kubeconform -strict -ignore-missing-schemas - && echo "PASS kubeconform"
else
  echo "skip kubeconform (not installed; brew install kubeconform for schema validation)"
fi

echo "all k8s checks passed"
```

```bash
chmod +x deploy/k8s/check.sh
```

- [ ] **Step 2: Add the Makefile target**

Add `k8s-check` to the `.PHONY` line at the top of `Makefile`, and add the target near `tf-check`:

```make
k8s-check:
	deploy/k8s/check.sh
```

- [ ] **Step 3: Run the gate**

Run: `make k8s-check`
Expected: PASS — base validates, `bash -n` clean, `render_test passed`, `smoke_test passed`, shellcheck/kubeconform either PASS or a "skip … (not installed)" line, then `all k8s checks passed`.

- [ ] **Step 4: Commit**

```bash
git add deploy/k8s/check.sh Makefile
git commit -m "Add make k8s-check: one offline gate for the deploy assets

Renders and client-validates the base, runs bash -n and the render/smoke logic
tests with only kubectl+jq+bash, and adds shellcheck/kubeconform when present."
```

---

## Task 8: Documentation

**Files:**
- Modify: `README.md` (the "Daily stack" section under Deploy)
- Modify: `CLAUDE.md` (Commands + Gotchas)

**Interfaces:**
- Consumes: the finished `deploy.sh`, `smoke.sh`, `make k8s-check`.
- Produces: the owner-facing bring-up/teardown runbook and the agent-facing gotchas.

- [ ] **Step 1: Extend the README "Daily stack" runbook**

In `README.md`, in the `### Daily stack (owner, each working session)` block, after the `./verify.sh` line and before "Tear down when done", add the app deploy steps; and change the teardown to release the ALB first. Requires `helm` (`brew install helm`).

```markdown
Then deploy the app onto the fresh cluster (needs `helm`; `kubectl` context is
set by the script):

    cd ../..                     # repo root
    deploy/k8s/deploy.sh up      # or: deploy/k8s/deploy.sh up <image-tag>
    deploy/k8s/smoke.sh          # after up

`deploy.sh up` installs the AWS Load Balancer Controller, applies the workloads,
rebuilds the OpenSearch index from the S3 corpus (coupa/star/hr), and prints the
demo URL — an internet-facing HTTP ALB (`http://<name>.<region>.elb.amazonaws.com/`).
The images must already be in ECR (built and pushed in sub-project 4c).

Tear down when done — release the ALB before destroying the stack, or the
destroy hangs on subnet/ENI dependencies:

    deploy/k8s/deploy.sh down    # deletes the Ingress (releases the ALB) + workloads
    cd terraform/daily && terraform destroy
```

- [ ] **Step 2: Note the offline check near the other checks**

In `README.md` under Deploy where `make tf-check` is described, add a sentence:

```markdown
`make k8s-check` validates the 4b deploy assets offline (manifests render and
client-validate, the overlay renders from fixture outputs, and the deploy/smoke
scripts pass syntax and logic tests) — no AWS, no cluster.
```

- [ ] **Step 3: Update CLAUDE.md**

In `CLAUDE.md` under **Commands**, add:

```markdown
- Deploy (K8s, `deploy/k8s/`): `make k8s-check` is the offline gate. `deploy/k8s/deploy.sh up [tag]` brings the app up on the daily EKS cluster (needs `helm` + the 4a stack applied); `deploy/k8s/deploy.sh down` releases the ALB and deletes workloads (run before `terraform destroy`). `deploy/k8s/smoke.sh` checks a live deploy.
```

Under **Gotchas**, add:

```markdown
- The 4b ALB routes all traffic to the UI Service; the UI's nginx reverse-proxies `/v1` to chat-api (SSE is tuned there, not at the ALB). Do not add a second Ingress backend for chat-api.
- The reseed Job runs the distroless indexer image, which has no shell: the three teams run as sequential Pod containers (coupa/star as initContainers, hr as the main container), never a shell loop. Sequential ordering avoids two invocations racing to create the index.
- `KA_RELEVANCE_FLOOR` in `deploy/k8s/base/chat-api.yaml` is set to 0.81, which is calibrated for local nomic-768. It is provisional on Bedrock/Titan-v2 — recalibrate it with `/check-retrieval` against the running stack and update the manifest.
- `deploy/k8s/deploy.sh down` must run before `terraform destroy` on the daily stack: the Ingress owns a real ALB whose ENIs otherwise block subnet deletion (a 4c ordering concern; `down` is the primitive).
```

- [ ] **Step 4: Verify the docs and the gate**

Run:
```bash
grep -n "deploy/k8s/deploy.sh up" README.md
grep -n "k8s-check" README.md CLAUDE.md
grep -n "KA_RELEVANCE_FLOOR" CLAUDE.md
make k8s-check
```
Expected: each grep prints its line(s); `make k8s-check` ends with `all k8s checks passed`.

- [ ] **Step 5: Commit**

```bash
git add README.md CLAUDE.md
git commit -m "Document the 4b deploy: bring-up/teardown runbook, k8s-check, gotchas

README gains the deploy.sh up/smoke/down steps in the daily-stack runbook;
CLAUDE.md gains the command summary and the ALB-routing, reseed-no-shell,
relevance-floor and teardown-ordering gotchas."
```

---

## Self-Review

**Spec coverage** (each spec section → task):
- chat-api/ui manifests, SAs, single replica → Task 2. Ingress (internet-facing, UI backend) → Task 3. reseed Job (init-container form) → Task 3.
- `ka-config` ConfigMap (opensearch_url + docs_bucket) → generated in Task 4, consumed by Tasks 2–3.
- ALB controller Helm install into the pre-bound SA → Task 5. `deploy.sh` render/up/down → Tasks 4–5. Anthropic secret via Pod Identity (literal secret id) → Task 2.
- Offline validation (`make k8s-check`) → Task 7; render + smoke offline tests → Tasks 4, 6. Post-deploy smoke → Task 6.
- 4a touch-ups (namespace tfvars + `docs_bucket`/`ecr_repository_urls`/`vpc_id` outputs, updated tests) → Task 1.
- Non-goals (4c image build, DNS/TLS, GitLab, HA) are honored: no task builds images, requests certs, deploys the GitLab CronJob, or adds replicas/HPA.
- Docs (README runbook, CLAUDE gotchas) → Task 8.

**Placeholder scan:** No TBD/TODO/"add error handling" left; every code step has literal content. The only intentional literals are the pinned `LBC_CHART_VERSION` (with a verify note) and `KA_RELEVANCE_FLOOR=0.81` (flagged provisional in-manifest and in CLAUDE.md).

**Type/name consistency:** kustomize image names `ka/chat-api`, `ka/ui`, `ka/ingestion` match between the base manifests (Task 2/3) and the overlay transformer (Task 4). ConfigMap `ka-config` keys `opensearch_url`/`docs_bucket` match between the generator (Task 4) and every `configMapKeyRef` (Tasks 2–3). SA names `chat-api`/`ingestion`/`aws-load-balancer-controller` match 4a's Pod Identity associations. Resource names `chat-api`/`ui` Deployments, `ka` Ingress, `reseed` Job match between manifests, `deploy.sh` (Task 5) and `smoke.sh` (Task 6).
