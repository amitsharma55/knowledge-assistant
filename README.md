# Knowledge Assistant

Chat-based assistant that answers questions about internal integrations,
grounded in markdown documentation stored in a GitLab repo and/or project wiki.

- Architecture: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- Chat API (Go): [`services/chat-api/`](services/chat-api/)
- Ingestion (Go): [`services/ingestion/`](services/ingestion/)
- UI (React): [`ui/`](ui/)
- Deploy (K8s): [`deploy/k8s/`](deploy/k8s/)

## Local dev

### Prerequisites

- **Docker Desktop** â runs OpenSearch, LocalStack (S3), Postgres, and the
  embedding Ollama.
- **Go** and **Node** â chat-api and the indexer run on the host, not in
  containers.
- **Ollama.app** (optional) â only if you want reranking or follow-up
  rewriting, which need `gpt-oss:20b`. See [Two Ollamas](#two-ollamas).

### Starting from scratch

Every step, in order, from a machine that has just rebooted:

```sh
# 1. config: copy the template and add your Anthropic key
cp .env.example .env   # skip if you already have a .env

# 2. start Docker Desktop itself, then wait for the daemon to accept
#    connections -- `make dev-up` fails outright if it is not up yet
open -a Docker
until docker info >/dev/null 2>&1; do sleep 1; done

# 3. OpenSearch + LocalStack (S3) + Postgres + Ollama
make dev-up

# 4. seed sample docs and index them
#    (depends on embed-pull, which downloads ~270MB on first run)
make seed

# 5. run chat-api
make run-api

# 6. in another shell
make run-ui
# open http://localhost:5173
```

Steps 1 and 4 are one-time-ish: after a reboot with the Docker volumes
intact, `make dev-up && make run-api` plus `make run-ui` is enough. `make
dev-down` passes `-v` and destroys the volumes, so the next start needs the
full sequence again.

Set `KA_LLM_MODE=mock` to skip Bedrock/Anthropic calls; the API returns
canned answers using retrieved chunks so you can iterate on retrieval
without spending tokens.

### Two Ollamas

Local dev can involve **two separate Ollama servers**, and they hold
different models:

| Port | Server | Holds | Used for | Config |
|---|---|---|---|---|
| `11435` | docker-compose `ollama` | `nomic-embed-text` | embeddings | `KA_OLLAMA_URL` |
| `11434` | native Ollama.app | `gpt-oss:20b` | rerank, rewrite | `KA_RERANK_URL`, `KA_REWRITE_URL` |

The container is on 11435 deliberately. If both listen on 11434 there is
**no bind error** â the app binds IPv4 `127.0.0.1:11434` and a Docker port
binding takes IPv6 `[::]:11434`, so they coexist â but `localhost` resolves
IPv4-first, so every embed call silently reaches the app, which does not
have `nomic-embed-text`:

```
retrieve: embed: ollama embed: http://localhost:11434/api/embed returned 404:
{"error":"model \"nomic-embed-text\" not found, try pulling it first"}
```

That error means requests are going to the **wrong Ollama**, not that the
pull failed â `make embed-pull` puts the model in the container, where
`ollama list` on the host cannot see it (the host CLI talks to the app).
To tell the two apart:

```sh
lsof -nP -iTCP:11434 -sTCP:LISTEN        # two LISTEN lines = the collision
docker exec docker-ollama-1 ollama list  # what the container has
ollama list                              # what the app has
```

If you do not need reranking, quit Ollama.app and set `KA_RERANK_MODE=off`
and `KA_REWRITE_MODE=off`; only the container is needed. Note that
Ollama.app installs itself as a login item and restarts with your Mac, so
it can reappear after a reboot even if you quit it.

### Ingesting from S3 (the AWS/EKS environment)

Documents live in the docs bucket under one prefix per team
(`s3://$KA_DOCS_BUCKET/<team>/`). The indexer reads a single team's prefix per
run; team is passed explicitly and never inferred from a key. PDF, Markdown and
plain text are ingested (via `internal/extract`).

Owner-run (needs AWS credentials):

    export KA_DOCS_BUCKET=ka-docs-<account-id>
    make seed-s3                                   # one-time: load the fixture corpus
    indexer -source s3 -team coupa -bucket $KA_DOCS_BUCKET
    indexer -source s3 -team star  -bucket $KA_DOCS_BUCKET
    indexer -source s3 -team hr    -bucket $KA_DOCS_BUCKET

`make seed-s3` copies `fixtures/<team>/` to each prefix; the fixtures already
sit under `fixtures/{coupa,star,hr}/`.

## Embeddings

Text is embedded by a local Ollama container (`nomic-embed-text`, 768
dimensions), so local dev needs no API key and costs nothing per token.
`make embed-pull` fetches the model; `make seed` depends on it. Note that
`make dev-down` passes `-v` and removes the volume holding it, so the next
pull re-downloads ~270MB.

| Variable | Default | Meaning |
|---|---|---|
| `KA_EMBED_MODE` | `ollama` | `ollama`, `bedrock`, or `mock` |
| `KA_OLLAMA_URL` | `http://localhost:11434` | Ollama endpoint. The compiled-in default predates the port split; set it to `http://localhost:11435` (as `.env.example` does) to reach the compose container. See [Two Ollamas](#two-ollamas). |
| `KA_EMBED_MODEL` | `nomic-embed-text` | Model name |
| `KA_EMBED_DIM` | `768` | Vector width; must match the model |

`KA_EMBED_MODE=mock` uses hash vectors with no semantic content. It exists
for tests and for exercising the pipeline offline â retrieval results under
it are meaningless, so it is never the default and an unknown mode is a
startup error rather than a silent fall back to it.

`KA_RELEVANCE_FLOOR` (default `0.81`) is a cutoff on the `(1+cos)/2` scale
both retrievers report. It gates only the cross-team suggestion â chunks
below it still reach the model, and the refusal wording comes from the
prompt.

The value is calibrated against `fixtures/tests.jsonl`: across 36 questions
over the 23-document corpus, the 30 answerable ones score no lower than
0.8421 and the 6 that are not answerable in the asking team score no higher
than 0.7739. The default is the midpoint of that gap. Re-derive it with
`python3 scripts/check_corpus.py --scores` after changing `KA_EMBED_MODEL`
or the corpus â absolute cosine thresholds do not transfer between models,
and embeddings are anisotropic, so unrelated text sits nowhere near 0.5.

Both chat-api and the indexer build their embedder from `embed.New`, so they
cannot be configured onto different models: mismatched document and query
vectors would return confident, unrelated results with no error anywhere.

The vector width is fixed in the index mapping when the index is created.
Changing `KA_EMBED_MODEL` to a model of a different width therefore requires
a reindex â `EnsureIndex` refuses to proceed and tells you so rather than
dropping your corpus:

```sh
curl -X DELETE http://localhost:9200/kb-chunks && make seed
```

## Teams

Every request is scoped to one team: `coupa`, `star`, or `hr`. `GET
/v1/teams` returns only the caller's own teams (`[{"slug", "displayName"},
...]`, always an array); every other request must send `X-Team: <slug>`
identifying which of those teams to query. There is no default team â a
request with no `X-Team` header is rejected (`401`/`400`, not silently
scoped to "everything"), and a team the caller does not belong to gets a
`403` that is deliberately indistinguishable from an unknown team, so a
caller can't learn which teams exist by probing. A chat's team is fixed at
creation; switching teams in the UI starts a new chat rather than
re-scoping the current one.

In dev, identity comes from the `X-Dev-User` header (`middleware.Auth`
reads it; there is no real JWT verification outside prod). The demo
membership table (`middleware.DemoMembers`) is:

| `X-Dev-User`        | Teams              |
|----------------------|--------------------|
| `a@example.com`      | coupa              |
| `b@example.com`      | star, hr           |
| `dev@example.com` (default, no header sent) | coupa, star, hr |

**There is no prod mode yet â do not deploy this as-is.**
`cmd/server/main.go` hardcodes `middleware.Auth(true /* dev */)`, and dev
`Auth` trusts `X-Dev-User` verbatim with no signature or session check
behind it. That means the entire team boundary described above currently
rests on a single client-supplied, spoofable header: anyone who can reach
the API can set `X-Dev-User: dev@example.com` and get every team (coupa,
star, hr), no ingestion access controls notwithstanding. This is fine for
local development and demos behind a trusted network, but wiring real JWT
verification (and a real prod auth mode) is required, and out of scope
for this branch, before this is exposed to any untrusted network.

Team is derived from the SharePoint site a document lives in, not from
per-item permissions â **item-level SharePoint ACLs are not mirrored**, so
a document restricted to a subset of a team is visible to that entire
team once ingested.

### The `suggestion` escape hatch needs real embeddings

When a query scores below `KA_RELEVANCE_FLOOR` in the active team but
would have scored above it in another team the caller belongs to, the
`suggestion` SSE event offers to switch. This does **not** fire under the
mock embedder (`KA_EMBED_MODE=mock`): mock embeddings carry no semantic
signal, so nothing clears the floor in any team â every query scores
around 0.5 on the `(1+cos)/2` scale (cosine â 0), far below the default
floor of `0.81`. Don't demo this feature without real embeddings (the
default Ollama `nomic-embed-text`, or Titan); it needs actual semantic
similarity to separate "no results here" from "results next door."

### `KA_RELEVANCE_FLOOR` is embedder-relative, not a portable constant

Both retrievers report the same scale: OpenSearch's `cosinesimil` k-NN
space returns `(1 + cos) / 2` (range `[0, 1]`), and the in-memory fixtures
store converts its cosine to match. A floor therefore carries over between
fixtures mode and OpenSearch, but not between embedding models: each model
spreads similarities differently, so a floor tuned on `nomic-embed-text`
must be re-derived with `check_corpus.py --scores` after switching to
another embedder such as Titan.

### Recalibrating for a new embedder (owner-run, needs AWS)

Titan v2 (1024-dim, normalized) has a different cosine distribution than
`nomic-embed-text` (768-dim), so `KA_RELEVANCE_FLOOR` must be re-derived and
the index reseeded. Neither CI nor an offline session can do this â it needs
live Bedrock credentials.

1. Export the Bedrock embed settings and AWS credentials:
   `KA_EMBED_MODE=bedrock KA_EMBED_MODEL=amazon.titan-embed-text-v2:0 KA_EMBED_DIM=1024 AWS_REGION=us-east-1`.
2. `make seed` â drops the 768-dim index first, so the width change is clean.
3. `python3 scripts/check_corpus.py --scores` â read the lowest answerable
   top score and the highest not-answerable-here top score.
4. Set `KA_RELEVANCE_FLOOR` to the midpoint of that gap (the same method the
   `0.81` default used for `nomic-embed-text`).
5. `python3 scripts/check_corpus.py --validate` to confirm the split.

### Reindexing after this change

`EnsureIndex` now refuses to start against an index whose mapping
predates the `team` field, since filtered k-NN search requires it and
patching the mapping in place would leave already-indexed docs with no
team value. If chat-api or the indexer fails at startup with an error
like `index "kb-chunks" already exists but its mapping has no "team"
field`, delete and reindex â this destroys existing indexed content, so
do it deliberately:

```sh
curl -X DELETE localhost:9200/kb-chunks
make seed
```

## Deploy

Everything in AWS is managed with Terraform under `terraform/`, run from your
laptop; GitHub Actions has no AWS access. Design:
`docs/superpowers/specs/2026-09-10-aws-foundation-design.md`.

- `terraform/bootstrap` â the S3 bucket that holds Terraform state (its own
  state is local).
- `terraform/foundation` â everything permanent: network, docs bucket, ECR, the
  Claude key secret, IAM roles, budget.
- `terraform/daily` â the environment created each morning and destroyed each
  evening (not built yet).

All configuration is in each stack's `terraform.tfvars` (committed) and
`private.auto.tfvars` (gitignored: account ID, alert email). Change values
there, never in `.tf` files; `make tf-check` fails if a tfvars value appears in
code. `make tf-check` runs every offline check and needs no AWS credentials.

### Prerequisites

```sh
brew uninstall terraform && brew install tfenv
tfenv install 1.5.7 && tfenv use 1.5.7   # the default outside this repo
tfenv install                            # in the repo: reads .terraform-version
```

AWS CLI credentials for the account listed in `allowed_account_ids`.

### One-time setup

```sh
cd terraform/bootstrap
cp private.auto.tfvars.example private.auto.tfvars      # fill in
terraform init && terraform apply
terraform output -raw state_bucket_name

cd ../foundation
cp private.auto.tfvars.example private.auto.tfvars      # fill in
cp backend.hcl.example backend.hcl                      # bucket from above
terraform init -backend-config=backend.hcl
terraform plan -out=foundation.tfplan                   # review: creates only
terraform apply foundation.tfplan
```

If `apply` fails on `aws_iam_service_linked_role.opensearch` with
`InvalidInput`/`EntityAlreadyExists`, the account already has the OpenSearch
service-linked role (any prior OpenSearch/Elasticsearch VPC domain creates it).
Import it and re-apply:

```sh
terraform import aws_iam_service_linked_role.opensearch \
  "$(aws iam get-role --role-name AWSServiceRoleForAmazonOpenSearchService \
       --query 'Role.Arn' --output text)"
```

Set the Claude API key once. It never reaches Terraform state, git or your
shell history:

```sh
read -rs KEY && aws secretsmanager put-secret-value \
  --secret-id "$(terraform output -raw anthropic_secret_name)" \
  --secret-string "$KEY"; unset KEY
```

Then check the result, and again after any IAM change:

```sh
./verify.sh
```

### Removing the foundation

`prevent_destroy` blocks destroying the state bucket, the docs bucket and the
secret. To remove everything deliberately:

1. Set `prevent_destroy = false` in `terraform/modules/private-bucket/main.tf`
   and `terraform/foundation/secrets.tf` (a local change; do not commit it).
2. Empty the docs bucket, including all object versions (S3 console: Empty).
3. Delete the images in the ECR repositories.
4. `terraform -chdir=terraform/foundation destroy`.
5. Empty the state bucket, including all versions, then
   `terraform -chdir=terraform/bootstrap destroy`.

### Daily stack (owner, each working session)

The daily stack holds the expensive, disposable compute â the EKS cluster and
the OpenSearch domain. Bring it up at the start of a session and destroy it at
the end; the document corpus lives in S3 (foundation) and the OpenSearch index
is rebuilt from it on each bring-up.

    cd terraform/daily
    cp private.auto.tfvars.example private.auto.tfvars   # set account values
    cp backend.hcl.example backend.hcl                   # set the state bucket
    terraform init -backend-config=backend.hcl
    terraform apply
    ./verify.sh                                          # after apply

Then deploy the app onto the fresh cluster (needs `helm`; `deploy.sh` sets the
kubectl context). The images must already be in ECR (built in sub-project 4c):

    cd ../..                     # repo root
    deploy/k8s/deploy.sh up      # or: deploy/k8s/deploy.sh up <image-tag>
    deploy/k8s/smoke.sh          # after up

`deploy.sh up` installs the AWS Load Balancer Controller, applies the workloads,
rebuilds the OpenSearch index from the S3 corpus (coupa/star/hr), and prints the
demo URL — an internet-facing HTTP ALB. `make k8s-check` validates these
deploy assets offline (manifests render, the overlay renders from fixture
outputs, the scripts pass syntax and logic tests) — no AWS, no cluster.

Tear down when done — release the ALB before destroying the stack, or the
destroy hangs on subnet/ENI dependencies:

    deploy/k8s/deploy.sh down    # deletes the Ingress (releases the ALB) + workloads
    cd terraform/daily && terraform destroy
