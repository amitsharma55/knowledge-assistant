# Bedrock Model Clients

Date: 2026-09-12
Status: Approved for planning

## Problem

The assistant's retrieval and answer pipeline runs entirely on locally hosted
models: two Ollamas provide embeddings (`nomic-embed-text`), reranking and
follow-up rewriting (`gpt-oss:20b`), and the answer is generated either by a
mock or by the direct Anthropic API. None of that works inside the AWS/EKS
daily environment, where there is no Ollama and no key on disk.

The permanent foundation (sub-project 2, applied and verified) already provisions
what a Bedrock-backed pipeline needs: an IAM role for the chat-api pod that may
invoke `amazon.titan-embed-text-v2:0` and `openai.gpt-oss-20b-1:0` and read the
Claude key from Secrets Manager, and an ingestion role that may invoke Titan and
read the documents bucket. Neither role may invoke Claude on Bedrock. The
application code to use any of this does not exist: `services/chat-api/internal/bedrock`
is commented-out scaffolding, and the only real embed/rerank/rewrite backends are
Ollama.

This spec covers sub-project 1 of the roadmap in
`docs/superpowers/specs/2026-09-10-aws-foundation-design.md`:

1. **Bedrock model clients — this spec.**
2. Permanent foundation (done).
3. S3 ingestion source.
4. Daily stack (EKS, OpenSearch domain, deploy, up/down).

## Goals

- The indexer and chat-api can run their existing pipeline against Bedrock:
  Titan Text Embeddings v2 for embeddings, gpt-oss-20b for reranking and
  follow-up rewriting.
- The answer LLM is served by the **direct Anthropic API** using the Claude key
  read from Secrets Manager. This matches the foundation IAM exactly (the pod may
  read the key; it may not invoke Claude on Bedrock).
- Backend selection is per component, via environment variables, with no model
  choice hardcoded. Local defaults (Ollama / mock) are unchanged.
- All new code is unit-tested offline with no AWS credentials and no live calls,
  so `go test ./...` and `go vet ./...` stay green in CI and for Claude.
- The index-time and query-time embedders remain guaranteed identical, as today.

## Non-Goals

- **Bedrock as the answer LLM.** Rejected: the foundation grants the pod the
  Claude key, not a Claude model on Bedrock. Choosing Bedrock Claude would reopen
  sub-project 2 (new IAM grant, re-verify). The answer path stays on the Anthropic
  API.
- **The daily stack, image build/push, deploy manifests' non-config parts, and
  DNS/TLS** (sub-project 4). This spec only fixes the *config* (env) block of the
  existing chat-api manifest so it stops contradicting the design; it leaves the
  `<account>` image and host placeholders for sub-project 4.
- **The S3 ingestion source** (sub-project 3). The indexer keeps its current
  `gitlab | fixtures` sources; only its embedder changes.
- **Live validation and relevance-floor recalibration.** These require real
  Bedrock and are the owner's to run (see "Owner-run validation"). Their outcome,
  not this code, sets the final `KA_RELEVANCE_FLOOR`.
- **Request signing for OpenSearch.** Unchanged; the client stays unsigned inside
  the VPC, as the foundation decided.

## Decisions

- **Approach: sibling backends next to each interface.** A `bedrock.go` joins the
  existing `ollama.go` in `internal/embed`, `internal/rerank`, and
  `internal/rewrite`, selected by a new `"bedrock"` mode in the same factory and
  config switches. This follows the established pattern (a backend lives beside
  its interface) and keeps the shared library usable by both binaries. Rejected: a
  single `internal/bedrock` package implementing all three interfaces (couples
  three unrelated concerns and breaks the pattern).
- **All three backends now, not Titan-only.** They share the SDK plumbing, and the
  foundation already grants gpt-oss to the pod, so the marginal cost over
  Titan-only is small. Rerank and rewrite default to `off`, so they never block a
  first deploy.
- **Credentials come from the SDK default chain.** In EKS, Pod Identity exposes
  credentials through the container-credentials endpoint that
  `config.LoadDefaultConfig` reads automatically. No credential code, no static
  keys.
- **Shared prompt/parse logic is extracted, not duplicated.** The gpt-oss prompt
  construction and reasoning-preamble stripping in the Ollama rerank/rewrite are
  transport-independent. They move into unexported helpers so `ollama.go` and
  `bedrock.go` differ only in the call.
- **The dead Bedrock-LLM code is removed, the mock is kept.**
  `services/chat-api/internal/bedrock/bedrock.go` (scaffolding for the rejected
  path) is deleted; `mock.go`'s `MockLLM` — which `KA_LLM_MODE=mock` uses — stays.
  The `bedrock` branch of the `LLMMode` switch is removed, so `LLMMode` is
  `mock | anthropic`.

## Design

### Components

| Unit | Responsibility | Used by |
|---|---|---|
| `internal/embed/bedrock.go` | Titan v2 embedder; `Embed(ctx, text) ([]float32, error)` | indexer + chat-api |
| `internal/rerank/bedrock.go` | gpt-oss-20b reranker; `Rerank(ctx, query, chunks)` | chat-api |
| `internal/rewrite/bedrock.go` | gpt-oss-20b rewriter; `Rewrite(ctx, question, history)` | chat-api |
| `internal/awsx` | Builds a `bedrockruntime` client via `LoadDefaultConfig` (region from `AWS_REGION`) | the three backends |
| chat-api secret load | On startup, populate the Anthropic key from Secrets Manager when `KA_ANTHROPIC_SECRET_ID` is set | chat-api |

Each Bedrock backend takes the runtime client behind a one-method `invoker`
interface (`InvokeModel`), so tests substitute a stub. The Secrets Manager read
sits behind a one-method `secretGetter` interface for the same reason.

### Titan embedder

`embed.New` gains a `"bedrock"` case; `OptionsFromEnv` reads `KA_EMBED_MODE=bedrock`.
`InvokeModel` on `amazon.titan-embed-text-v2:0` with payload
`{"inputText": text, "dimensions": 1024, "normalize": true}`; the response's
`embedding` field is returned as `[]float32`, and `New` reports dim 1024. Because
the width changes 768 → 1024, `EnsureIndex` refuses to reuse the existing index —
forcing the deliberate drop-and-reseed described below, which is the intended
safeguard, not a bug to work around.

### gpt-oss reranker and rewriter

`RerankMode` / `RewriteMode` gain a `"bedrock"` case constructing the new backend
against `openai.gpt-oss-20b-1:0`. The prompt and the reasoning-strip parser are
shared with the Ollama backend via the extracted helpers. gpt-oss on Bedrock is
invoked through the Bedrock **Converse** API (or `InvokeModel` with the OpenAI
chat schema); the exact request shape is confirmed against current AWS
documentation during implementation, behind the same `invoker` seam. The
reranker keeps its existing safety net: a dropped or duplicated id from the model
must never drop or duplicate a chunk.

### Answer LLM and the Claude key

The answer path is unchanged code
(`services/chat-api/internal/anthropic/claude.go`, `KA_LLM_MODE=anthropic`). The
only change is the key's source: when `KA_ANTHROPIC_SECRET_ID` is set, chat-api
reads the secret value at startup and uses it as `AnthropicAPIKey`; otherwise it
falls back to the environment key for local dev. The existing empty-key guard
stays, so a missing or unreadable key stops the process rather than degrading it.

### Configuration surface

Local defaults are unchanged; the EKS environment selects Bedrock via env.

| Var | Local default | EKS (daily) |
|---|---|---|
| `KA_EMBED_MODE` | `ollama` | `bedrock` |
| `KA_EMBED_MODEL` / `KA_EMBED_DIM` | `nomic-embed-text` / 768 | `amazon.titan-embed-text-v2:0` / 1024 |
| `KA_RERANK_MODE` / `KA_REWRITE_MODE` | `off` | `bedrock` |
| `KA_RERANK_MODEL` / `KA_REWRITE_MODEL` | `gpt-oss:20b` | `openai.gpt-oss-20b-1:0` |
| `KA_LLM_MODE` | `mock` | `anthropic` |
| `KA_ANTHROPIC_SECRET_ID` | unset (env key) | `ka/anthropic-api-key` |
| `AWS_REGION` | — | `us-east-1` |

### Data flow

Unchanged pipeline; only the backend implementations swap.

- Indexing: indexer → `embed.New(bedrock)` → Titan → 1024-dim vectors → OpenSearch.
- Query: chat-api → rewrite (Bedrock gpt-oss) → embed query (Titan) → OpenSearch
  kNN → rerank (Bedrock gpt-oss) → context → Anthropic API (key from Secrets
  Manager) → streamed answer.

The invariant that index-time and query-time vectors come from one model holds:
both binaries build the embedder from `embed.New(embed.OptionsFromEnv())`, and the
index width guard blocks any mismatch.

### Error handling

Errors wrap with a package prefix and read for an operator
(`fmt.Errorf("pkg: context: %w", err)`).

- Throttling relies on the AWS SDK's built-in adaptive retryer; a persistent
  failure surfaces as, e.g., `embed: bedrock titan invoke: %w`.
- If `KA_ANTHROPIC_SECRET_ID` is set but the secret cannot be read, chat-api
  refuses to start — the same fail-fast philosophy as the mock-embedder guard.
- Unknown mode strings keep their existing explicit errors.

## Testing

Offline only, matching the repo's `httptest` + hand-written `stubX` convention.

- Titan: a stub `invoker` asserts the request payload
  (`inputText` / `dimensions` / `normalize`) and that the `embedding` field parses
  to `[]float32`.
- gpt-oss rerank/rewrite: the extracted prompt-build and reasoning-strip helpers
  are unit-tested directly; a stub `invoker` covers the transport.
- Secrets Manager: a stub `secretGetter` covers the startup load and the
  fail-fast on error.
- No test makes a live AWS call. `go test ./...` and `go vet ./...` pass without
  credentials.

## Owner-run validation

The live half of "done", run by the owner with AWS credentials (like `verify.sh`
was for the foundation). Documented near the existing retrieval-check docs:

1. With `KA_EMBED_MODE=bedrock` and the Titan model/dim set, reseed the index
   (`make seed` drops first, so the 768 → 1024 width change is clean).
2. Run `scripts/check_corpus.py --scores` to read the new cosine distribution.
3. Pick and set `KA_RELEVANCE_FLOOR` for the Titan backend; the current `0.81` is
   calibrated for `nomic-embed-text` and will not transfer.
4. Re-run `scripts/check_corpus.py --validate` to confirm.

## File structure

| Path | Change |
|---|---|
| `internal/embed/bedrock.go` | New: Titan v2 embedder |
| `internal/embed/new.go` | Add `"bedrock"` case + options |
| `internal/rerank/bedrock.go` | New: gpt-oss reranker |
| `internal/rerank/ollama.go` | Extract shared prompt/parse helpers |
| `internal/rewrite/bedrock.go` | New: gpt-oss rewriter |
| `internal/rewrite/ollama.go` | Extract shared prompt/parse helpers |
| `internal/awsx/` | New: shared `bedrockruntime` client builder |
| `services/chat-api/internal/config/config.go` | Add `KA_ANTHROPIC_SECRET_ID`; drop `bedrock` from `LLMMode` and remove the now-dead `BedrockModelID`/`BedrockRegion` fields. Rerank/rewrite already read model id/url/timeout from env, so no new fields there; the `"bedrock"` mode string is handled in `main.go`. Embedder settings stay in `internal/embed`, deliberately absent here. |
| `services/chat-api/cmd/server/main.go` | Secret load at startup; remove `bedrock` LLM branch; wire `"bedrock"` rerank/rewrite cases; Bedrock region for all three backends comes from `internal/awsx` (`AWS_REGION`) |
| `services/chat-api/internal/bedrock/bedrock.go` | Delete (dead scaffolding) |
| `services/ingestion/cmd/indexer/main.go` | Embedder already via `embed.New`; no change beyond env |
| `deploy/k8s/chat-api.yaml` | Fix env block to the real config surface (placeholders for `<account>`/host remain) |
| `go.mod` / `go.sum` | Add `aws-sdk-go-v2/config`, `service/bedrockruntime`, `service/secretsmanager` |
| `*_test.go` (embed, rerank, rewrite, config) | Offline tests with stub `invoker` / `secretGetter` |
| retrieval-check docs | Add the owner-run recalibration procedure |
