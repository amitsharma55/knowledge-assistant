# Knowledge Assistant

A chat-based assistant that answers business users' questions about internal
integrations, grounded in markdown documentation. Every answer is retrieved
from a per-team corpus and cites its sources, so users can verify. It is pure
RAG — retrieve, then answer — with no write path and no agent loops.

- **What it is** and how the pieces fit: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- **Chat API** (Go): [`services/chat-api/`](services/chat-api/) — retrieve → prompt → stream (SSE)
- **Ingestion** (Go): [`services/ingestion/`](services/ingestion/) — chunk → embed → index
- **UI** (React + Vite): [`ui/`](ui/) — chat surface, renders citations
- **Deploy** (Kubernetes/EKS): [`deploy/k8s/`](deploy/k8s/)
- **Infra** (Terraform): [`terraform/`](terraform/)

This README is the runbook. Read [Local dev](#local-dev) to run it on your
laptop, and [Deploy to AWS](#deploy-to-aws) to bring up the cloud
environment. The [Reference](#reference) section at the end explains the
retrieval knobs and the two-Ollama gotcha in depth — you don't need it to get
started.

---

## What runs where

| Piece | Local dev | AWS |
|---|---|---|
| chat-api (Go) | on the host, `:8080` | EKS Deployment |
| ingestion indexer (Go) | on the host, one run per team | EKS Job (reseed) |
| UI (React) | Vite dev server, `:5173` | container behind an ALB |
| Vector + text index | OpenSearch in Docker, `:9200` | OpenSearch (daily stack) |
| Chat history | Postgres in Docker, `:5432` | Postgres (StatefulSet, daily) |
| Doc storage | fixtures on disk (LocalStack S3 at `:4566`) | S3 docs bucket (foundation) |
| Embeddings | Ollama container `nomic-embed-text`, `:11435` | Bedrock Titan Embed v2 |
| Answer LLM | Anthropic API (or `mock`) | Bedrock (Claude / gpt-oss) |
| Rerank / rewrite (optional) | native Ollama.app `gpt-oss:20b`, `:11434` | Bedrock |

Local dev needs no AWS account and no cloud spend. Retrieval runs entirely on
your machine; only the answer LLM calls out (and you can set `mock` to avoid
even that).

---

## Local dev

### Prerequisites

Install these once:

- **Docker Desktop** — runs OpenSearch, LocalStack (S3), Postgres, and the
  embedding Ollama. Everything else runs on the host.
- **Go 1.25+** — chat-api and the indexer (`go version`; the module targets
  `go 1.25.0`).
- **Node 18+ and npm** — the UI (`node --version`; Vite 5, React 18).
- **Ollama.app** *(optional)* — only for reranking / follow-up rewriting,
  which need the `gpt-oss:20b` model. Skip it and set `KA_RERANK_MODE=off` /
  `KA_REWRITE_MODE=off`. See [Two Ollamas](#two-ollamas).
- An **Anthropic API key** *(optional)* — only if you want real answers.
  Without one, run in `KA_LLM_MODE=mock` and the API returns canned answers
  built from the retrieved chunks, so you can work on retrieval for free.

Ports used on the host: `8080` (chat-api), `5173` (UI), `9200` (OpenSearch),
`5432` (Postgres), `4566` (LocalStack S3), `11435` (embedding Ollama),
`11434` (native Ollama.app, optional). Free them up before starting.

### First run, step by step

Every step, in order, from a machine that has just rebooted:

```sh
# 1. Config: copy the template and add your Anthropic key (or leave the
#    placeholder and use mock mode — see step 5). .env is gitignored.
cp .env.example .env            # skip if you already have a .env

# 2. Start Docker Desktop, then wait for the daemon to accept connections.
#    `make dev-up` fails outright if the daemon is not up yet.
open -a Docker
until docker info >/dev/null 2>&1; do sleep 1; done

# 3. Bring up the backing services: OpenSearch + LocalStack + Postgres + Ollama.
make dev-up

# 4. Seed the sample corpus and index it. This pulls the embedding model on
#    first run (~270MB), drops any existing index, indexes the three teams'
#    fixtures, and refreshes so they are immediately searchable.
make seed

# 5. Run chat-api (reads .env). To skip all LLM calls, prefix KA_LLM_MODE=mock.
make run-api

# 6. In a second shell, run the UI.
make run-ui
#    Then open http://localhost:5173
```

### After a reboot

Steps 1 and 4 are one-time-ish. As long as the Docker volumes survive, a
normal start is just:

```sh
make dev-up      # services back up
make run-api     # in one shell
make run-ui      # in another
```

If a question then fails with `opensearch 404: no such index [kb-chunks]`, the
OpenSearch volume did not survive the reboot (only the index is gone; the
services are up). Re-run `make seed` to rebuild it — it is idempotent, and the
embedding model re-download is skipped if it is still cached:

```sh
make seed        # drops + reindexes the fixtures
```

> ⚠️ `make dev-down` passes `-v` and **deletes the Docker volumes** — the
> OpenSearch index and the pulled embedding model. After it, the next start
> needs the full first-run sequence again (including the ~270MB re-download in
> `make seed`).

### Verify it works

With chat-api running (`:8080`), dev identity comes from the `X-Dev-User`
header and the team from `X-Team` (see [Teams](#teams)):

```sh
# Which teams does this dev user belong to? (default dev user = all three)
curl -s localhost:8080/v1/teams -H 'X-Dev-User: dev@example.com'

# A grounded question against the coupa corpus (SSE stream).
curl -N localhost:8080/v1/chat/messages \
  -H 'X-Dev-User: dev@example.com' -H 'X-Team: coupa' \
  -H 'Content-Type: application/json' \
  -d '{"message":"How does invoice matching work?"}'
```

Or just use the UI at http://localhost:5173, pick a team, and ask.

To measure retrieval quality against the fixture corpus after a change, use
the `/check-retrieval` skill (wraps `python3 scripts/check_corpus.py` against a
running chat-api). It is a 36-question sanity check, not an eval harness — a
1–2 question swing is noise.

### Trace the retrieval pipeline

`python3 scripts/trace.py` renders each query's pipeline steps (retrieved,
reranked, context, response) as it runs. It tails
`${TMPDIR:-/tmp}/ka-dev/chat-api.log`, which **only exists if chat-api was
started via the `/run-local` skill** — plain `make run-api` logs to its own
terminal instead, and trace.py reports `no log at …/chat-api.log`. To use
trace.py without the skill, start the API redirected to that file, with
`KA_TRACE=1` for per-chunk scores and the full prompt/response:

```sh
mkdir -p "${TMPDIR:-/tmp}/ka-dev"
KA_TRACE=1 make run-api > "${TMPDIR:-/tmp}/ka-dev/chat-api.log" 2>&1
python3 scripts/trace.py   # in another shell, then ask a question
```

### Tests, lint, build

```sh
go test ./...                    # Go unit tests
go vet ./...
make lint                        # golangci-lint v2 (config in .golangci.yml)
npm --prefix ui test             # UI tests (Vitest + React Testing Library)
npm --prefix ui run build        # clean UI build
make build                       # static chat-api + indexer binaries into bin/
```

---

## Configuration

Local config lives in `.env` (gitignored; copy from `.env.example`). `make
run-api` and `make seed` both source it. The variables that matter most:

| Variable | Default | Meaning |
|---|---|---|
| `KA_LLM_MODE` | `anthropic` | Answer LLM adapter: `mock`, `anthropic`, or `bedrock`. `mock` returns canned answers with no API call. |
| `ANTHROPIC_API_KEY` | — | Required when `KA_LLM_MODE=anthropic`. |
| `KA_ANTHROPIC_MODEL` | `claude-sonnet-4-5-20250929` | Model used in `anthropic` mode. |
| `KA_EMBED_MODE` | `ollama` | Embedding backend: `ollama`, `bedrock`, or `mock`. |
| `KA_OLLAMA_URL` | `http://localhost:11435` | Embedding Ollama. **Must be 11435**, not 11434 — see [Two Ollamas](#two-ollamas). |
| `KA_OPENSEARCH_URL` | `http://localhost:9200` | Vector + text index. |
| `KA_OPENSEARCH_INDEX` | `kb-chunks` | Index name. |
| `KA_POSTGRES_DSN` | `postgres://ka:ka@localhost:5432/ka?sslmode=disable` | Chat-history store (matches the compose Postgres). |
| `KA_RELEVANCE_FLOOR` | `0.81` | Cutoff on the `(1+cos)/2` scale; **embedder-specific**. See [Relevance floor](#relevance-floor). |
| `KA_RERANK_MODE` | `off` | `ollama` reorders the retrieval pool; `off` keeps vector order. |
| `KA_REWRITE_MODE` | `off` | `ollama` rewrites follow-ups into standalone queries. |
| `KA_ADMIN_USERS` | — | Comma-separated `X-Dev-User` allowlist for the upload review queue (see [Document upload](#document-upload-pii-review)). |
| `KA_ADDR` | `:8080` | chat-api listen address. |

`KA_TOP_K`, `KA_RERANK_TOP_N`, `KA_MAX_CONTEXT`, and the `KA_RERANK_*` /
`KA_REWRITE_*` timeouts and URLs tune the retrieval pipeline; `.env.example`
documents each inline.

> `make run-api` sources `.env`, whose `KA_LLM_MODE` overrides anything you set
> on the command line. Edit `.env`, or invoke `go run ./services/chat-api/cmd/server`
> directly, to override it.

---

## How documents get in

The corpus is markdown, organized as one **team** per source. There are three
teams: `coupa`, `star`, `hr`. The indexer (`services/ingestion/cmd/indexer`)
ingests one team per run, and team is always passed explicitly — never
inferred from a key or path.

- **Local (`-source fixtures`)** — `make seed` indexes `fixtures/{coupa,star,hr}/`
  straight from disk. `fixtures/` is fabricated demo data: self-contained `##`
  sections under ~800 words, facts as bullets.
- **AWS (`-source s3`)** — documents live in the docs bucket, one prefix per
  team (`s3://$KA_DOCS_BUCKET/<team>/`). Owner-run, needs AWS credentials:

  ```sh
  export KA_DOCS_BUCKET=ka-docs-<account-id>
  make seed-s3                                    # one-time: copy fixtures up
  indexer -source s3 -team coupa -bucket $KA_DOCS_BUCKET
  indexer -source s3 -team star  -bucket $KA_DOCS_BUCKET
  indexer -source s3 -team hr    -bucket $KA_DOCS_BUCKET
  ```

  On the daily EKS stack this runs as a reseed Job, not by hand — `deploy.sh up`
  triggers it.

PDF, Markdown, and plain text are extracted (`internal/extract`). Chunk
document ids hash the chunk text, so **any change to the chunker or embedding
needs a full reseed** (`make seed` drops the index first) — otherwise stale
chunks rank against fresh ones.

### Document upload (PII review)

The UI also has an upload path: a user can add a PDF / Office / text document,
which is scanned for PII before anything reaches the knowledge base. Documents
flagged as sensitive are held in a review queue and only indexed after a human
approves them. The reviewer is any user whose `X-Dev-User` is in the
`KA_ADMIN_USERS` allowlist. Sensitive data never reaches the index without
approval.

---

## Teams

Every request is scoped to exactly one team. `GET /v1/teams` returns only the
caller's own teams; every other request must send `X-Team: <slug>`. There is
no default team — a request with no `X-Team` is rejected, and a team the caller
does not belong to returns a `403` deliberately indistinguishable from an
unknown team, so probing can't reveal which teams exist. A chat's team is fixed
at creation; switching teams in the UI starts a new chat.

In **dev**, identity comes from the `X-Dev-User` header (`middleware.Auth`
reads it verbatim — there is no JWT verification). The demo membership table:

| `X-Dev-User` | Teams |
|---|---|
| `a@example.com` | coupa |
| `b@example.com` | star, hr |
| `dev@example.com` *(default, no header sent)* | coupa, star, hr |

> **There is no prod auth mode yet — do not expose this to an untrusted
> network.** `cmd/server/main.go` runs `middleware.Auth(true /* dev */)`, which
> trusts `X-Dev-User` with no signature check. The entire team boundary rests
> on a spoofable client header: anyone who reaches the API can set
> `X-Dev-User: dev@example.com` and read every team. Fine for local dev and
> demos behind a trusted network; real JWT verification is required before any
> real exposure. Read
> `docs/superpowers/specs/2026-08-30-team-segregation-design.md` before
> changing retrieval scoping.

---

## Deploy to AWS

AWS is managed with Terraform under `terraform/`, run from the owner's laptop
(GitHub Actions has no AWS access). There are three stacks:

- **`terraform/bootstrap`** — the S3 bucket that holds Terraform state (its own
  state is local). One-time.
- **`terraform/foundation`** — everything permanent: network, docs bucket, ECR,
  the Claude key secret, IAM roles, budget. One-time.
- **`terraform/daily`** — the disposable stack (EKS + OpenSearch) created each
  working session and destroyed each evening. The document corpus lives in S3
  (foundation); the daily OpenSearch index is rebuilt from it on each bring-up.

All configuration is in each stack's `terraform.tfvars` (committed) and
`private.auto.tfvars` (gitignored: account id, alert email) — never literals in
`.tf` files (`make tf-check` enforces this). `make tf-check` runs every offline
check and needs no AWS credentials.

> Never run `terraform apply`, `destroy`, `import`, or `init` against the real
> backend unless asked; use `init -backend=false` for offline work. The daily
> stack must never be left applied overnight.

### Prerequisites

```sh
brew uninstall terraform && brew install tfenv
tfenv install 1.5.7 && tfenv use 1.5.7   # default outside this repo
tfenv install                            # in the repo: reads .terraform-version
```

Also: the **AWS CLI** with credentials for the account in `allowed_account_ids`,
**helm**, and **kubectl**. `make images` additionally needs Docker with
`buildx`.

### One-time setup (bootstrap + foundation)

```sh
# 1. State bucket (bootstrap keeps its own state locally).
cd terraform/bootstrap
cp private.auto.tfvars.example private.auto.tfvars      # fill in
terraform init && terraform apply
terraform output -raw state_bucket_name

# 2. Foundation (permanent infra). Generate the backend config from bootstrap:
cd ../.. && make tf-backend                             # writes foundation/backend.hcl
cd terraform/foundation
cp private.auto.tfvars.example private.auto.tfvars      # fill in
terraform init -backend-config=backend.hcl
terraform plan -out=foundation.tfplan                   # review: creates only
terraform apply foundation.tfplan
```

If `apply` fails on `aws_iam_service_linked_role.opensearch` with
`InvalidInput` / `EntityAlreadyExists`, the account already has the OpenSearch
service-linked role. Import it and re-apply:

```sh
terraform import aws_iam_service_linked_role.opensearch \
  "$(aws iam get-role --role-name AWSServiceRoleForAmazonOpenSearchService \
       --query 'Role.Arn' --output text)"
```

Set the Claude API key once — it never reaches Terraform state, git, or your
shell history:

```sh
read -rs KEY && aws secretsmanager put-secret-value \
  --secret-id "$(terraform output -raw anthropic_secret_name)" \
  --secret-string "$KEY"; unset KEY
```

Then verify (and again after any IAM change):

```sh
./verify.sh
```

### A normal day (the daily stack)

The daily stack holds the expensive, disposable compute. The whole day is two
commands from the repo root:

```sh
deploy/k8s/day.sh up      # build+push images ‖ terraform apply, verify, deploy, print URL
deploy/k8s/day.sh down    # release the ALB, then destroy the daily stack
```

`day.sh up` builds and pushes all three images (`make images`, `linux/amd64`)
in parallel with `terraform -chdir=terraform/daily apply` — the build hides
behind OpenSearch creation — then runs `verify.sh` and `deploy.sh up latest`.
`deploy.sh up` installs the AWS Load Balancer Controller, applies the
workloads, rebuilds the OpenSearch index from the S3 corpus, and prints the
demo URL (an internet-facing HTTP ALB). `day.sh down` releases the ALB
**before** `terraform destroy` — the Ingress owns a real ALB whose ENIs
otherwise block subnet deletion. Both terraform calls use `-auto-approve` and
target only the daily stack; foundation is never touched.

Check a live deploy with `deploy/k8s/smoke.sh`.

<details>
<summary>Doing it in steps instead of <code>day.sh</code></summary>

```sh
# Bring the infra up:
cd terraform/daily
cp private.auto.tfvars.example private.auto.tfvars   # set account values
cp backend.hcl.example backend.hcl                   # set the state bucket
terraform init -backend-config=backend.hcl
terraform apply
./verify.sh

# Deploy the app onto the fresh cluster (images must already be in ECR — `make images`):
cd ../..
deploy/k8s/deploy.sh up        # or: deploy/k8s/deploy.sh up <image-tag>
deploy/k8s/smoke.sh

# Tear down — release the ALB before destroying, or the destroy hangs:
deploy/k8s/deploy.sh down
cd terraform/daily && terraform destroy
```
</details>

### Forgotten-teardown safety net (macOS)

So a stack left up overnight does not bill continuously:

```sh
deploy/k8s/reaper/install.sh    # LaunchAgent: runs `day.sh reap` at 22:00 local, daily
deploy/k8s/reaper/uninstall.sh  # remove it
deploy/k8s/day.sh keep          # skip tonight's reaper (late demo)
```

`day.sh reap` tears the stack down if a live cluster is detected and no
keep-flag is set for today. Logs and flags live under `~/.ka/`. The LaunchAgent
runs via a login shell so it inherits your AWS profile and PATH — a minimal
launchd environment is why a fresh install should be verified once on your own
machine.

### Offline gates (no AWS, no cluster)

```sh
make tf-check     # terraform fmt, validate, `terraform test` (mocked provider), script tests
make k8s-check    # manifests render, overlay renders from fixtures, deploy scripts pass bash -n/shellcheck + logic tests
```

### Removing the foundation

`prevent_destroy` blocks destroying the state bucket, docs bucket, and secret.
To remove everything deliberately:

1. Set `prevent_destroy = false` in `terraform/modules/private-bucket/main.tf`
   and `terraform/foundation/secrets.tf` (local change; do not commit).
2. Empty the docs bucket, including all object versions.
3. Delete the images in the ECR repositories.
4. `terraform -chdir=terraform/foundation destroy`.
5. Empty the state bucket (all versions), then
   `terraform -chdir=terraform/bootstrap destroy`.

---

## Reference

You don't need this section to run the app — it explains the retrieval knobs
and the local-dev gotchas in depth.

### Two Ollamas

Local dev can involve **two separate Ollama servers** holding different models:

| Port | Server | Holds | Used for | Config |
|---|---|---|---|---|
| `11435` | docker-compose `ollama` | `nomic-embed-text` | embeddings | `KA_OLLAMA_URL` |
| `11434` | native Ollama.app | `gpt-oss:20b` | rerank, rewrite | `KA_RERANK_URL`, `KA_REWRITE_URL` |

The container is on 11435 deliberately. If both listen on 11434 there is **no
bind error** — the app binds IPv4 `127.0.0.1:11434` and a Docker port binding
takes IPv6 `[::]:11434`, so they coexist — but `localhost` resolves IPv4-first,
so every embed call silently reaches the app, which lacks `nomic-embed-text`:

```
retrieve: embed: ollama embed: http://localhost:11434/api/embed returned 404:
{"error":"model \"nomic-embed-text\" not found, try pulling it first"}
```

That error means requests hit the **wrong Ollama**, not that the pull failed —
`make embed-pull` puts the model in the container, where `ollama list` on the
host cannot see it. To tell the two apart:

```sh
lsof -nP -iTCP:11434 -sTCP:LISTEN        # two LISTEN lines = the collision
docker exec docker-ollama-1 ollama list  # what the container has
ollama list                              # what the app has
```

If you don't need reranking, quit Ollama.app and set `KA_RERANK_MODE=off` and
`KA_REWRITE_MODE=off`; only the container is needed. Ollama.app installs itself
as a login item, so it can reappear after a reboot even if you quit it.

### Embeddings

Text is embedded by the local Ollama container (`nomic-embed-text`, 768
dimensions), so local dev needs no API key and costs nothing per token.
`make embed-pull` fetches it; `make seed` depends on it.

| Variable | Default | Meaning |
|---|---|---|
| `KA_EMBED_MODE` | `ollama` | `ollama`, `bedrock`, or `mock` |
| `KA_OLLAMA_URL` | `http://localhost:11434` | Set to `http://localhost:11435` (as `.env.example` does) to reach the compose container. |
| `KA_EMBED_MODEL` | `nomic-embed-text` | Model name |
| `KA_EMBED_DIM` | `768` | Vector width; must match the model |

`KA_EMBED_MODE=mock` uses hash vectors with no semantic content — for tests and
offline pipeline exercises only. Retrieval under it is meaningless, so it is
never the default and an unknown mode is a startup error, never a silent
fallback.

Both chat-api and the indexer build their embedder from `embed.New`, so they
cannot be configured onto different models — mismatched document and query
vectors would return confident, unrelated results with no error anywhere.

The vector width is fixed in the index mapping at creation. Changing
`KA_EMBED_MODEL` to a model of a different width requires a reindex;
`EnsureIndex` refuses to proceed (rather than dropping your corpus) and tells
you so. It also refuses an index whose mapping predates the `team` field.
The fix is a deliberate delete + reseed:

```sh
curl -X DELETE http://localhost:9200/kb-chunks && make seed
```

### Relevance floor

`KA_RELEVANCE_FLOOR` (default `0.81`) is a cutoff on the `(1+cos)/2` scale that
both retrievers report (OpenSearch's `cosinesimil` k-NN space and the in-memory
fixtures store both use it). It gates only the cross-team `suggestion` event —
chunks below it still reach the model; the refusal wording comes from the
prompt.

The `0.81` default is calibrated for `nomic-embed-text` against
`fixtures/tests.jsonl`: the answerable questions score no lower than ~0.842 and
the not-answerable-here ones no higher than ~0.774, and the default is the
midpoint. **The floor is embedder-specific and does not transfer between
models** — absolute cosine thresholds don't carry over, and embeddings are
anisotropic (unrelated text sits nowhere near 0.5). Re-derive it after changing
the embedder or the corpus:

```sh
python3 scripts/check_corpus.py --scores      # read the answerable / not-answerable gap
# set KA_RELEVANCE_FLOOR to the midpoint, then:
python3 scripts/check_corpus.py --validate
```

The cross-team `suggestion` escape hatch (offering to switch to a team where
the query *would* clear the floor) needs real embeddings — under
`KA_EMBED_MODE=mock` every query sits near 0.5, so nothing clears any floor.
Don't demo it without a real embedder.

#### Recalibrating for Bedrock / Titan v2 (owner-run, needs AWS)

Titan v2 (1024-dim, normalized) has a different cosine distribution than
`nomic-embed-text`, so the floor must be re-derived and the index reseeded.
CI and offline sessions can't do this — it needs live Bedrock credentials.

1. Export the Bedrock embed settings and AWS credentials:
   `KA_EMBED_MODE=bedrock KA_EMBED_MODEL=amazon.titan-embed-text-v2:0 KA_EMBED_DIM=1024 AWS_REGION=us-east-1`.
2. `make seed` — drops the 768-dim index first, so the width change is clean.
3. `python3 scripts/check_corpus.py --scores` — read the gap.
4. Set `KA_RELEVANCE_FLOOR` to the midpoint.
5. `python3 scripts/check_corpus.py --validate` to confirm the split.

On EKS the floor lives in `deploy/k8s/base/chat-api.yaml`; recalibrate against
the running stack and update the manifest.
