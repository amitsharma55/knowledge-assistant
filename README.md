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

- **Docker Desktop** — runs OpenSearch, LocalStack (S3), Postgres, and the
  embedding Ollama.
- **Go** and **Node** — chat-api and the indexer run on the host, not in
  containers.
- **Ollama.app** (optional) — only if you want reranking or follow-up
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
**no bind error** — the app binds IPv4 `127.0.0.1:11434` and a Docker port
binding takes IPv6 `[::]:11434`, so they coexist — but `localhost` resolves
IPv4-first, so every embed call silently reaches the app, which does not
have `nomic-embed-text`:

```
retrieve: embed: ollama embed: http://localhost:11434/api/embed returned 404:
{"error":"model \"nomic-embed-text\" not found, try pulling it first"}
```

That error means requests are going to the **wrong Ollama**, not that the
pull failed — `make embed-pull` puts the model in the container, where
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

## Embeddings

Text is embedded by a local Ollama container (`nomic-embed-text`, 768
dimensions), so local dev needs no API key and costs nothing per token.
`make embed-pull` fetches the model; `make seed` depends on it. Note that
`make dev-down` passes `-v` and removes the volume holding it, so the next
pull re-downloads ~270MB.

| Variable | Default | Meaning |
|---|---|---|
| `KA_EMBED_MODE` | `ollama` | `ollama` or `mock` |
| `KA_OLLAMA_URL` | `http://localhost:11434` | Ollama endpoint. The compiled-in default predates the port split; set it to `http://localhost:11435` (as `.env.example` does) to reach the compose container. See [Two Ollamas](#two-ollamas). |
| `KA_EMBED_MODEL` | `nomic-embed-text` | Model name |
| `KA_EMBED_DIM` | `768` | Vector width; must match the model |

`KA_EMBED_MODE=mock` uses hash vectors with no semantic content. It exists
for tests and for exercising the pipeline offline — retrieval results under
it are meaningless, so it is never the default and an unknown mode is a
startup error rather than a silent fall back to it.

`KA_RELEVANCE_FLOOR` (default `0.81`) is a cutoff on the `(1+cos)/2` scale
both retrievers report. It gates only the cross-team suggestion — chunks
below it still reach the model, and the refusal wording comes from the
prompt.

The value is calibrated against `fixtures/tests.jsonl`: across 36 questions
over the 23-document corpus, the 30 answerable ones score no lower than
0.8421 and the 6 that are not answerable in the asking team score no higher
than 0.7739. The default is the midpoint of that gap. Re-derive it with
`python3 scripts/check_corpus.py --scores` after changing `KA_EMBED_MODEL`
or the corpus — absolute cosine thresholds do not transfer between models,
and embeddings are anisotropic, so unrelated text sits nowhere near 0.5.

Both chat-api and the indexer build their embedder from `embed.New`, so they
cannot be configured onto different models: mismatched document and query
vectors would return confident, unrelated results with no error anywhere.

The vector width is fixed in the index mapping when the index is created.
Changing `KA_EMBED_MODEL` to a model of a different width therefore requires
a reindex — `EnsureIndex` refuses to proceed and tells you so rather than
dropping your corpus:

```sh
curl -X DELETE http://localhost:9200/kb-chunks && make seed
```

## Teams

Every request is scoped to one team: `coupa`, `star`, or `hr`. `GET
/v1/teams` returns only the caller's own teams (`[{"slug", "displayName"},
...]`, always an array); every other request must send `X-Team: <slug>`
identifying which of those teams to query. There is no default team — a
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

**There is no prod mode yet — do not deploy this as-is.**
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
per-item permissions — **item-level SharePoint ACLs are not mirrored**, so
a document restricted to a subset of a team is visible to that entire
team once ingested.

### The `suggestion` escape hatch needs real embeddings

When a query scores below `KA_RELEVANCE_FLOOR` in the active team but
would have scored above it in another team the caller belongs to, the
`suggestion` SSE event offers to switch. This does **not** fire under the
mock embedder (`KA_LLM_MODE=mock` / fixtures mode): mock embeddings carry
no semantic signal, so nothing clears the floor in any team — measured
scores land around 0.029 and -0.010 against the default floor of 0.25, in
every team, for every query. Don't demo this feature expecting it to
work without wiring up real embeddings (Bedrock Titan); it needs actual
semantic similarity to have any chance of separating "no results here"
from "results next door."

### `KA_RELEVANCE_FLOOR` is backend-relative, not a portable constant

The in-memory fixtures store reports raw cosine similarity (range
`[-1, 1]`), while OpenSearch's `cosinesimil` k-NN space reports a
normalized `(1 + cos) / 2` (range `[0, 1]`). The same `KA_RELEVANCE_FLOOR`
value therefore means something different in each mode, and must be
retuned per deployment rather than copied between them.

### Reindexing after this change

`EnsureIndex` now refuses to start against an index whose mapping
predates the `team` field, since filtered k-NN search requires it and
patching the mapping in place would leave already-indexed docs with no
team value. If chat-api or the indexer fails at startup with an error
like `index "kb-chunks" already exists but its mapping has no "team"
field`, delete and reindex — this destroys existing indexed content, so
do it deliberately:

```sh
curl -X DELETE localhost:9200/kb-chunks
make seed
```

## Deploy

Everything in AWS is managed with Terraform under `terraform/`, run from your
laptop; GitHub Actions has no AWS access. Design:
`docs/superpowers/specs/2026-09-10-aws-foundation-design.md`.

- `terraform/bootstrap` — the S3 bucket that holds Terraform state (its own
  state is local).
- `terraform/foundation` — everything permanent: network, docs bucket, ECR, the
  Claude key secret, IAM roles, budget.
- `terraform/daily` — the environment created each morning and destroyed each
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
