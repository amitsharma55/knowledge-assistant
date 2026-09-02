# Knowledge Assistant

Chat-based assistant that answers questions about internal integrations,
grounded in markdown documentation stored in a GitLab repo and/or project wiki.

- Architecture: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- Chat API (Go): [`services/chat-api/`](services/chat-api/)
- Ingestion (Go): [`services/ingestion/`](services/ingestion/)
- UI (React): [`ui/`](ui/)
- Deploy (K8s): [`deploy/k8s/`](deploy/k8s/)

## Local dev

```sh
# start OpenSearch + LocalStack (S3) + Postgres + Ollama
make dev-up

# seed sample docs and index them
# (depends on embed-pull, which downloads the embedding model on first run)
make seed

# run chat-api with mock Bedrock
make run-api

# in another shell
make run-ui
# open http://localhost:5173
```

Set `KA_LLM_MODE=mock` to skip Bedrock calls; the API returns canned answers
using retrieved chunks so you can iterate on retrieval without spending
tokens.

## Embeddings

Text is embedded by a local Ollama container (`nomic-embed-text`, 768
dimensions), so local dev needs no API key and costs nothing per token.
`make embed-pull` fetches the model; `make seed` depends on it. Note that
`make dev-down` passes `-v` and removes the volume holding it, so the next
pull re-downloads ~270MB.

| Variable | Default | Meaning |
|---|---|---|
| `KA_EMBED_MODE` | `ollama` | `ollama` or `mock` |
| `KA_OLLAMA_URL` | `http://localhost:11434` | Ollama endpoint |
| `KA_EMBED_MODEL` | `nomic-embed-text` | Model name |
| `KA_EMBED_DIM` | `768` | Vector width; must match the model |

`KA_EMBED_MODE=mock` uses hash vectors with no semantic content. It exists
for tests and for exercising the pipeline offline — retrieval results under
it are meaningless, so it is never the default and an unknown mode is a
startup error rather than a silent fall back to it.

`KA_RELEVANCE_FLOOR` (default `0.78`) is a cutoff on the `(1+cos)/2` scale
both retrievers report. It gates only the cross-team suggestion — chunks
below it still reach the model, and the refusal wording comes from the
prompt. The value is empirical and tied to the embedding model: with
`nomic-embed-text` over the fixtures, answerable questions score 0.82–0.94
and off-corpus ones still score 0.72–0.74, because embeddings are
anisotropic and unrelated text sits nowhere near 0.5. Re-derive it if you
change `KA_EMBED_MODEL`.

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

CI builds images to ECR on merge to `main` and rolls out via Helm; see
[`deploy/k8s/chart/`](deploy/k8s/chart/).
