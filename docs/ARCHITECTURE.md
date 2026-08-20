# Knowledge Assistant — Architecture

## 1. Purpose

A chat-based assistant that answers business users' questions about internal
integrations (e.g. *"What fields does the ServiceNow integration send?"*,
*"When does the nightly reconciliation job run?"*). Knowledge lives in
markdown files in a self-managed GitLab instance — repo docs (e.g. `docs/*.md`)
and/or project wiki pages. The assistant retrieves and cites source pages so
users can verify.

## 2. Non-goals (v1)

- Not a write path: the assistant does not modify integrations, trigger jobs,
  or edit tickets.
- No structured SQL over integration metadata (data source is only GitLab
  markdown — pure RAG).
- No multi-turn tool use / agent loops beyond retrieval-then-answer.

## 3. High-level diagram

```
┌───────────────┐    HTTPS       ┌──────────────┐          ┌────────────────────┐
│   Chat UI     │───────────────▶│  ALB + WAF   │─────────▶│  chat-api (Go)     │
│  (React,      │◀────SSE────────│  + Cognito   │◀─── SSE──│  EKS Deployment    │
│   container)  │                └──────────────┘          └────┬──────┬────────┘
└───────────────┘                                               │      │
                                                                │      │
                                    ┌───────────────────────────┘      │
                                    │                                  │
                                    ▼                                  ▼
                          ┌───────────────────┐              ┌──────────────────┐
                          │ Bedrock           │              │ OpenSearch       │
                          │  - Claude Sonnet  │              │ Serverless       │
                          │  - Titan Embed v2 │              │ (hybrid k-NN+BM25)│
                          └───────────────────┘              └────────▲─────────┘
                                                                      │ index
                                                                      │
                                                              ┌───────┴────────┐
                                                              │ ingestion job  │
                                                              │ (Go, EKS Cron) │
                                                              └───────▲────────┘
                                                                      │
                                            ┌─────────────────────────┼────────────┐
                                            │                         │            │
                                            ▼                         ▼            ▼
                                    ┌────────────────┐        ┌──────────────┐  ┌───────┐
                                    │ GitLab         │        │ S3 raw + norm│  │ RDS   │
                                    │ REST API       │        │ (snapshot,   │  │ Postgres
                                    │                │        │  markdown)   │  │ chats │
                                    └────────────────┘        └──────────────┘  └───────┘
```

## 4. Components

| Component        | Tech                          | Responsibility |
|------------------|-------------------------------|----------------|
| Chat UI          | React + Vite, containerized   | Chat surface, streams SSE, renders citations |
| chat-api         | Go 1.23, chi router           | Query orchestration: retrieve → prompt → stream |
| ingestion        | Go 1.23, EKS `CronJob`        | Pull GitLab repo docs + wiki pages, chunk, embed, index |
| Vector + text index | OpenSearch Serverless      | Hybrid k-NN + BM25 retrieval with metadata filters |
| Object storage   | S3                            | Raw HTML snapshot + normalized markdown per page |
| Metadata store   | RDS Postgres                  | Users, sessions, chat history, feedback, ingestion state |
| LLM + embeddings | Amazon Bedrock (Claude + Titan) | Answer synthesis, query embeddings |
| Cache            | ElastiCache Redis             | Session state, response cache, rate limiting |
| Auth             | Cognito (OIDC to corporate IdP) | User identity, group claims for space-level ACLs |
| Ingress          | ALB + WAF                     | TLS, WAF rules, path routing |
| Observability    | OpenTelemetry → CloudWatch + X-Ray | Traces per query, LLM cost metrics |

## 5. Data flow

### 5.1 Ingestion (batch, hourly `CronJob`)

1. For each configured GitLab project, list docs from both sources:
   - **Repo files**: `GET /api/v4/projects/:id/repository/tree?recursive=true&path=docs&ref=main`
     → filter to `*.md`/`*.markdown` blobs → `GET /repository/files/:path/raw?ref=main`.
   - **Wiki pages**: `GET /api/v4/projects/:id/wikis` → for each markdown page,
     `GET /api/v4/projects/:id/wikis/:slug?render_html=false`.
2. For each page:
   - Snapshot markdown to `s3://kb-norm/<project>:<source>/<id>.md`.
   - Chunk at ~800 tokens with 100-token overlap, on heading boundaries.
   - Embed each chunk with Titan Embeddings v2 (1024-dim).
   - Upsert into OpenSearch index `kb-chunks` with fields:
     `{id, spaceKey, pageId, pageTitle, sectionPath, url, updatedAt, text, embedding, aclGroups[]}`.
   - `spaceKey` = `<projectPath>:repo` or `<projectPath>:wiki`; `url` is the
     GitLab web URL so citations link back for verification.
3. Deletion handling: track page IDs seen; any indexed page not seen in two
   consecutive runs is soft-deleted.

Incremental strategy (v1): re-fetch all in-scope files and rely on
content-hashed `id` for idempotent upserts. v2: track last commit SHA per
project and diff commits since to fetch only changed files.

### 5.2 Query (interactive)

1. Client `POST /v1/chat/messages` with `{sessionId, message}`; server verifies
   JWT (Cognito), loads session + recent turns from Postgres, checks Redis
   response cache keyed by `hash(userGroups + normalizedQuery)`.
2. Query rewrite: Claude Haiku turns the user's turn into a standalone search
   query using recent history (cheap, fast).
3. Embed rewritten query with Titan; issue **hybrid search** to OpenSearch
   (k-NN over `embedding` + BM25 over `text`, RRF fusion), filter by
   `aclGroups ∩ user.groups`, top-K = 8.
4. Rerank top-K with a cross-encoder (Cohere Rerank via Bedrock) → top-4.
5. Build prompt: system rules + `<context>` blocks with per-chunk citation IDs
   + user question.
6. Stream Claude Sonnet response via Bedrock `InvokeModelWithResponseStream`;
   forward as SSE. Enforce: **only answer from context; cite `[n]` for every
   claim; say "I don't know" if the answer isn't in retrieved chunks.**
7. Persist turn + retrieved chunk IDs + token counts to Postgres for evals.

## 6. Key decisions and tradeoffs

### 6.1 Bedrock over self-hosted models
Chosen for: IAM-native auth, no data egress, no GPU ops, Guardrails.
Trade: per-token cost vs one-time GPU capex; acceptable at expected QPS
(<50 rps). Revisit if sustained load pushes monthly Bedrock cost past ~$5k.

### 6.2 OpenSearch Serverless over pgvector / Pinecone
Chosen for: hybrid retrieval in one system, AWS-native, no capacity planning.
pgvector rejected because we want BM25 alongside k-NN and don't want to run
two search paths. Pinecone rejected to avoid a second vendor and data egress.

### 6.3 Go over Java Spring Boot
Chosen for: small container image (~25 MB), fast start, first-class HTTP/SSE,
low memory footprint per pod (important for horizontal scale on EKS). AWS SDK
v2 for Go covers Bedrock + OpenSearch cleanly. Team must be comfortable in Go.

### 6.4 Pure RAG (no tool calling) in v1
Data source is only GitLab markdown — no structured metadata to query. Adding tool
calling later (e.g. a `list_jobs()` tool backed by the scheduler DB) is a
straight-line extension: swap the retrieval step for a tool-use loop.

### 6.5 Hybrid search + rerank
BM25 alone misses paraphrase; k-NN alone misses exact identifier matches
(e.g. field names like `u_incident_number`). Hybrid + rerank consistently
beats either alone on internal-doc QA benchmarks.

### 6.6 Response caching keyed on user groups
Two users with the same groups asking the same question get the same answer
from cache. Different group sets never share a cache entry — prevents ACL
leaks.

### 6.7 Citations are load-bearing
Every claim must carry a `[n]` citation resolving to a chunk with a
GitLab URL. If Claude cannot cite, it must refuse. This is enforced in
the system prompt and validated post-hoc; uncited answers are logged for eval.

## 7. Security

- All traffic through ALB + AWS WAF; WAF managed rules + rate limit per IP.
- Cognito user pool federated to corporate SAML/OIDC.
- JWT verified in chat-api middleware; user groups extracted from `cognito:groups`.
- OpenSearch queries always filtered by `aclGroups ∩ user.groups`; index-time
  ACL derived from GitLab project visibility + group membership.
- IAM roles for Service Accounts (IRSA) for pod → Bedrock / OpenSearch / S3.
- No secrets in env vars in cluster; use AWS Secrets Manager + CSI driver.
- Bedrock Guardrails: PII redaction, prompt injection detection, denied topics.

## 8. Observability + evals

- OpenTelemetry SDK in chat-api and ingestion; traces exported to X-Ray.
- Per-query span attributes: `retrieval.k`, `retrieval.scores`,
  `llm.input_tokens`, `llm.output_tokens`, `llm.cost_usd`, `answer.cited`.
- Golden eval set: 50 hand-labeled Q&A pairs from real integration docs;
  run nightly via `services/chat-api/cmd/eval`; report answer relevance,
  citation precision, refusal rate.
- Structured feedback: 👍/👎 on each answer persisted with turn ID for
  offline analysis.

## 9. Deployment

- EKS cluster, one namespace `knowledge-assistant`.
- Deployments: `chat-api` (HPA 2–10 pods), `ui` (2 pods behind ALB).
- `CronJob`: `ingestion-gitlab` every hour.
- Helm chart in `deploy/k8s/chart/`; GitHub Actions builds images, pushes to
  ECR, rolls out via `helm upgrade` on merge to `main`.
- Blue/green for chat-api via two Deployments + Service selector flip.

## 10. Rollout plan

| Phase | Scope | Exit criteria |
|-------|-------|---------------|
| 0 | Local prototype: docker-compose, mock Bedrock, sample docs | Query round-trip works end-to-end locally |
| 1 | Internal alpha: real Bedrock, one GitLab project, 5 users | ≥ 70% answer relevance on golden set |
| 2 | Beta: all integration spaces, 50 users, SSO on | ≥ 85% relevance, p95 latency < 4s |
| 3 | GA: HPA tuned, cost dashboards, on-call runbook | SLO 99.5%, monthly cost projection signed off |

## 11. Open questions

- GitLab auth: Project Access Token (service-account style, per-project) vs
  Personal Access Token vs OAuth 2.0 (per-user). PAT is simplest but loses
  per-user ACLs — need product decision on whether the assistant enforces
  the requesting user's GitLab visibility.
- Do we snapshot attachments (PDFs, diagrams)? v1 scope says no; if yes, add
  Textract + image captions pipeline.
- Retention policy for chat history? Default 90 days pending compliance review.
