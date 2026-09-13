# Application Deploy (Sub-project 4b)

Date: 2026-09-13
Status: Approved for planning

## Problem

Sub-project 4a stands up the disposable compute: an EKS cluster with one managed
node group, a single-node OpenSearch domain, the core add-ons, and three Pod
Identity associations that pre-bind the chat-api, ingestion, and load-balancer
controller service accounts to their foundation roles. It installs **no
workloads** — the cluster is empty and the OpenSearch domain has no index.

4b makes the assistant actually run on that cluster each morning: it deploys the
chat-api and UI workloads, installs the AWS Load Balancer Controller, rebuilds
the OpenSearch index from the S3 corpus, and exposes the app through an
internet-facing HTTP ALB so the owner can demo it from a laptop. Everything is
rendered from the 4a stack's `terraform output` and applied with `kubectl`/`helm`
by a single deploy script; no Kubernetes or Helm provider is pulled into
Terraform (a 4a decision, to keep 4a offline-testable).

## Operating model

Per 4a: the owner runs `terraform apply` on the daily stack in the morning and
`terraform destroy` in the evening. OpenSearch — and its index — is destroyed on
every teardown, so **the index is disposable and the S3 corpus (foundation) is
the source of truth**. 4b's reseed rebuilds the index from S3 on every bring-up.

4b's deploy runs *after* 4a's apply and *after* the container images exist in
ECR (image build/push is 4c). Its `down` primitive (delete the Ingress and
workloads) runs *before* 4a's `terraform destroy`, because the Ingress
provisions a real ALB whose ENIs otherwise block the cluster's subnet deletes.
Sequencing that ordering across layers is 4c's contract; 4b only provides the
primitive.

## Goals

- Kubernetes manifests for chat-api and the UI, their Services and
  ServiceAccounts, running in the `knowledge-assistant` namespace, sized for a
  solo demo (single replica, no HPA).
- A `ka-config` ConfigMap holding the two per-environment endpoints — the
  OpenSearch VPC URL (`opensearch_url`, read by chat-api and the reseed Job) and
  the docs bucket (`docs_bucket`, read by the reseed Job) — both non-sensitive.
  The Anthropic key is never in a manifest; it is read at runtime from Secrets
  Manager via Pod Identity, with the secret *name* (`ka/anthropic-api-key`) a
  stable manifest literal matching foundation's `anthropic_secret_name`.
- The AWS Load Balancer Controller installed by Helm into the
  `kube-system/aws-load-balancer-controller` service account that 4a already
  bound via Pod Identity.
- A one-shot reseed Job that rebuilds the index from S3 for all three teams
  (coupa, star, hr) on each bring-up.
- An internet-facing HTTP Ingress that routes all traffic to the UI Service; the
  UI's nginx reverse-proxies `/v1` to chat-api internally (SSE already tuned
  there).
- A `deploy.sh` that renders a kustomize overlay from `terraform output`,
  installs the controller, applies the workloads, runs the reseed Job, and prints
  the ALB URL; and a `deploy.sh down` primitive that releases the ALB.
- Offline validation with no credentials (`kustomize build` + `kubeconform` +
  `shellcheck`) and a post-deploy smoke check that needs a kubeconfig.
- Two 4a touch-ups: retarget the chat-api/ingestion namespaces to
  `knowledge-assistant` (tfvars + updated `terraform test` expectations), and add
  `docs_bucket`, `ecr_repository_urls`, and `vpc_id` pass-through outputs so
  `deploy.sh` reads only the daily stack.

## Non-Goals

- **Everything in 4c:** building and pushing images to ECR, the
  morning-up/evening-down orchestration sequence, and the forgotten-teardown
  safety net. 4b consumes images by tag and provides the `down` primitive; it
  does not sequence the day.
- **Real DNS and TLS.** The demo is HTTP-only on the ALB's generated
  `*.elb.amazonaws.com` name. No domain, no ACM certificate, no Route53 record.
  A future phase can add HTTPS on an owned domain.
- **GitLab ingestion.** The corpus is loaded from S3. The existing GitLab
  CronJob sketch is not deployed; GitLab remains a documented future source.
- **High availability / autoscaling.** Single chat-api replica, no HPA — this is
  a disposable demo, matching 4a's single-node posture.
- **Any Kubernetes or Helm provider in Terraform.** Workloads are applied by
  `kubectl`/`helm` from a script, keeping 4a AWS-provider-only and offline
  testable.
- **New IAM.** Foundation owns all roles; 4a created the Pod Identity
  associations. 4b creates only the (annotation-free) ServiceAccount objects the
  associations bind by name.
- **Authentication.** Dev identity via the `X-Dev-Groups` header and team
  selection via `X-Team`, exactly as local dev and the UI already work.

## Decisions

**The ALB routes everything to the UI; nginx proxies `/v1` to chat-api.** The UI
container already reverse-proxies `/v1/` to `chat-api:8080` with SSE-correct
settings (`proxy_buffering off`, `proxy_read_timeout 3600s`). Routing the ALB at
a single backend (the UI Service) avoids re-solving SSE streaming at the ALB
(idle-timeout and buffering annotations) and eliminates path-ordering surprises
between two Ingress backends. chat-api's Service stays ClusterIP-internal.

**`ka-config` is a ConfigMap, not a Secret.** It holds two non-sensitive
per-environment values — `opensearch_url` (the VPC endpoint, `https://` +
`opensearch_endpoint`) and `docs_bucket` (foundation's corpus bucket). chat-api
reads `opensearch_url`; the reseed Job reads both (`KA_OPENSEARCH_URL`,
`KA_DOCS_BUCKET`) via `configMapKeyRef`. The one real secret — the Anthropic API
key — never enters a manifest; chat-api reads it from Secrets Manager through Pod
Identity using `KA_ANTHROPIC_SECRET_ID`, whose value is the stable secret name
`ka/anthropic-api-key` (a manifest literal, matching foundation's
`anthropic_secret_name`).

**Single replica, no HPA.** A solo demo does not need two chat-api replicas or an
autoscaler. The sketch's `replicas: 2` + `HorizontalPodAutoscaler` are dropped.

**Render with kustomize, apply with a script.** `deploy/k8s/` holds plain base
manifests. `deploy.sh` reads `terraform output -json`, generates a small overlay
(an `images:` transformer for the two image refs + tag, a `configMapGenerator`
for `ka-config`, and the Ingress host/annotations), and `kubectl apply -k`s it.
This matches the repo's bash+jq script convention and keeps the base manifests
plain YAML that validates offline. envsubst was rejected (awkward for the
ConfigMap and harder to validate); a Helm chart was rejected as more machinery
than a solo demo needs (the controller is already a separate Helm release).

**One-shot reseed Job, three teams as sequential steps.** The `ingestion` image
is `distroless/static` — it has no shell, so a `sh -c 'for t in …'` loop is
impossible. The indexer also processes exactly one team per invocation. So the
reseed is a single Job whose Pod runs the indexer three times as **sequential
containers**: `coupa` and `star` as `initContainers` (which Kubernetes runs in
order, each to completion), and `hr` as the main container. All three share the
image and differ only in the `-team` argument. Sequential ordering matters:
`EnsureIndex` creates the index on a 404, so two invocations racing on an
empty index could both try to create it. A CronJob was rejected (a disposable
stack rebuilt each morning would re-index redundantly); three separate Jobs were
rejected because they would run `EnsureIndex` concurrently against an empty
index and race on creation. The Job's `restartPolicy` is `Never` with a small
`backoffLimit`; the deploy script waits for it to `Complete`.

**Images by tag from foundation's ECR outputs.** `deploy.sh` takes an image tag
argument (default `latest`) and combines it with `ecr_repository_urls`. Building
and pushing those images is 4c; 4b's runtime dependency on them is documented,
not owned.

**Two touch-ups to 4a, so deploy.sh reads one stack.** First, the pod
namespaces: 4a made them configurable (only the service-account *names* are
fixed literals), so 4b sets `chat_api_namespace` and `ingestion_namespace` to
`knowledge-assistant` in `terraform/daily/terraform.tfvars` (lb-controller stays
`kube-system`) and updates the 4a `terraform test` expectations — a tfvars edit,
not a `.tf` change. Second, 4a's `outputs.tf` exposes cluster/opensearch values
but not the two foundation values 4b also needs — the docs bucket (reseed) and
the ECR repository URLs (image refs). Both already live in `local.foundation`
(read via `terraform_remote_state`), so 4b adds three pass-through outputs
(`docs_bucket`, `ecr_repository_urls`, `vpc_id` — the last for the ALB
controller Helm install) to `terraform/daily/outputs.tf`. Then
`deploy.sh` reads only the daily stack's outputs, exactly as `verify.sh` does,
rather than opening a second remote-state read of its own.

## Component layout

```
deploy/k8s/
  namespace.yaml        knowledge-assistant namespace
  serviceaccounts.yaml  chat-api, ingestion (annotation-free; Pod Identity binds by name)
  chat-api.yaml         Deployment (1 replica) + ClusterIP Service :8080
  ui.yaml               Deployment + ClusterIP Service :80
  ka-config… (generated) ConfigMap via kustomize configMapGenerator
  reseed-job.yaml        one-shot Job, ingestion SA, coupa/star as initContainers + hr main
  ingress.yaml           internet-facing HTTP ALB -> ui Service
  kustomization.yaml     base; deploy.sh writes a generated overlay on top
  deploy.sh              render overlay from tf outputs, helm install controller, apply, reseed, print URL; `down` releases the ALB
  smoke.sh               post-deploy live checks (needs kubeconfig)
```

The AWS Load Balancer Controller service account lives in `kube-system` and is
created by the Helm release (`serviceAccount.create=false` with the fixed name,
so it matches 4a's association).

## Workloads

- **chat-api:** Deployment, 1 replica, SA `chat-api`. Env: `KA_LLM_MODE=anthropic`,
  `KA_ANTHROPIC_SECRET_ID`, `AWS_REGION`, the Bedrock embed/rerank/rewrite settings
  (`KA_EMBED_MODE=bedrock`, `amazon.titan-embed-text-v2:0`, dim 1024;
  `KA_RERANK_MODE`/`KA_REWRITE_MODE=bedrock`, gpt-oss), `KA_OPENSEARCH_URL` from
  the `ka-config` ConfigMap, and `KA_RELEVANCE_FLOOR` calibrated for the Titan
  embedder (confirm the Bedrock-path value; local dev's 0.81 is for nomic-768).
  ClusterIP Service on 8080. `/healthz` readiness and liveness probes.
- **ui:** Deployment + ClusterIP Service on 80. nginx serves the static build and
  proxies `/v1` to chat-api.
- **reseed Job:** SA `ingestion`, `restartPolicy: Never`, small `backoffLimit`.
  The distroless image has no shell, so the three teams run as sequential Pod
  containers: `coupa` and `star` as `initContainers`, `hr` as the main
  container, each `args: [-source, s3, -team, <slug>, -bucket, $(KA_DOCS_BUCKET)]`
  (same image, different `-team`). `KA_DOCS_BUCKET` from foundation's
  `docs_bucket`; `KA_OPENSEARCH_URL` from `ka-config`; Bedrock embed env matching
  chat-api so the indexed and query vectors come from one model.

## Ingress & load balancer controller

- **Controller:** `helm upgrade --install aws-load-balancer-controller
  eks/aws-load-balancer-controller` (chart version pinned in `deploy.sh`). Values:
  `clusterName`, `region`, `vpcId` from tf outputs; `serviceAccount.create=false`,
  `serviceAccount.name=aws-load-balancer-controller`. Pod Identity (from 4a) gives
  the SA its permissions; no IRSA annotation is needed.
- **Ingress:** `ingressClassName: alb`, `alb.ingress.kubernetes.io/scheme:
  internet-facing`, `target-type: ip`, HTTP:80 listener. A single default backend
  → the `ui` Service on 80. Placed in foundation's public subnets (already tagged
  `kubernetes.io/role/elb=1`), so the controller provisions an internet-facing
  ALB without extra subnet tagging. The generated ALB DNS name is the demo URL;
  `deploy.sh` waits for `.status.loadBalancer.ingress[0].hostname` and prints it.

## Deploy & teardown flow

`deploy.sh [up|down] [image-tag]`:

1. **up (default):** `terraform -chdir=terraform/daily output -json` →
   cluster/opensearch/region/ecr/docs-bucket values; `aws eks update-kubeconfig`;
   `helm upgrade --install` the controller and wait for it Ready; write the
   generated kustomize overlay (images + tag, `ka-config` configMapGenerator,
   Ingress); `kubectl apply -k`; wait for chat-api and ui rollouts; create the
   reseed Job and wait for `Complete`; wait for the Ingress ALB address; print it.
2. **down:** delete the Ingress (releasing the ALB and its ENIs), then delete the
   workloads. This is the primitive 4c's evening-down runs before `terraform
   destroy`; 4b does not itself sequence the Terraform teardown.

## Testing & validation

- **Offline (no credentials), a new `make k8s-check` (or folded into existing
  checks):** `kustomize build` the base + a fixture overlay with placeholder
  output values; `kubeconform` (or `kubectl apply --dry-run=client`) over the
  rendered YAML; `shellcheck` on `deploy.sh` and `smoke.sh`.
- **4a regression:** after the tfvars namespace change, `make tf-check` still
  passes and the Pod Identity `terraform test` asserts the
  `knowledge-assistant` namespace for chat-api/ingestion.
- **Post-deploy smoke (`smoke.sh`, needs kubeconfig/credentials, not in
  offline checks):** controller, chat-api and ui Deployments Ready; reseed Job
  `Complete`; Ingress has an ALB hostname; `GET /healthz` returns 200; a sample
  `POST /v1/chat/messages` with `X-Team: coupa` and `X-Dev-Groups: everyone`
  returns a grounded answer. Optionally run `scripts/check_corpus.py` against the
  ALB URL as the retrieval sanity check (not an eval; a 1–2 question swing is
  noise).
- **No Go changes expected.** The reseed loop lives in the Job manifest, not new
  Go. If the container image lacks a shell for the loop, the fallback is three
  sequential Jobs rather than adding Go.

## Dependencies & risks

- **Images must exist in ECR** (4c) before a real `deploy.sh up`; offline
  validation and manifest review do not need them.
- **`KA_RELEVANCE_FLOOR` is embedder-calibrated.** The Bedrock/Titan path may need
  a different floor than local nomic-768; the plan must verify the deployed value
  rather than copy 0.81 blindly.
- **ALB teardown ordering** is a cross-layer hazard owned by 4c; 4b's `down`
  primitive and its documentation are the mechanism 4c relies on.
