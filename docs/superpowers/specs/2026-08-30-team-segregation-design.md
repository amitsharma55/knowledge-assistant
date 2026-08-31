# Per-Team Data Segregation

Date: 2026-08-30
Status: Approved for planning

## Problem

The assistant will serve several teams — initially Coupa, Star, and HR — whose
documentation, membership, and confidentiality expectations differ. A Coupa
user must not retrieve HR content, and answers must not be diluted by chunks
from corpora the user did not ask about.

Today all chunks share one index and one undifferentiated `aclGroups` field,
retrieval applies that field as an OpenSearch `post_filter`, and nothing in the
type system prevents a retrieval path from skipping the filter entirely.

## Decisions

Settled during brainstorming; recorded here so the plan does not relitigate them.

1. **Isolation level: real boundary, single index.** Team is derived
   server-side from identity, enforced at one chokepoint every retrieval path
   must cross, and covered by tests that assert cross-team leakage returns
   nothing. Not index-per-team: the operational cost is not justified at this
   size, and a single index keeps a future cross-team capability open.
2. **Content source: SharePoint**, superseding GitLab as the primary corpus.
   One SharePoint site (or document library) per team. **No SharePoint site is
   available for the demo**: documents are placed directly in S3 under a
   per-team prefix, and the SharePoint crawler is added later as a producer
   into the same S3 layout. Because S3 is already the seam between fetching
   bytes and indexing them, nothing downstream of it changes when the crawler
   arrives.
3. **Content is staged in S3 before indexing**, not ingested directly from
   Graph into the vector store.
4. **Identity: Entra ID group claims**, with a hardcoded resolver for the demo
   behind the same interface.
5. **Embeddings are not partitioned.** One model, one vector space, one index.
   Segregation is a partition-and-filter concern, not an embedding concern.
6. **One team per chat.** A user belonging to several teams picks one when
   starting a chat; the chat is pinned to it. See *Multi-team users* below.
7. **Retrieved context is displayed** in a collapsible panel showing exactly
   what the model received. See *Retrieval transparency* below.

## Non-Goals

- **No cross-team or "All teams" search.** A query is scoped to exactly one
  team, including for users who belong to several.
- **No reranking.** `RerankTopN` currently truncates kNN results rather than
  reranking them. Left as is; noted because the transparency panel makes it
  visible.
- **No mirroring of SharePoint item-level or folder-level permissions.** Team
  is derived from the site; a document restricted to a subset of a team's
  members becomes visible to that entire team. This limitation must be stated
  in the README, because it is the failure mode enterprise RAG deployments most
  often ship by accident.
- **No per-team embedding models or per-team index tuning.**
- **No migration of existing indexed data.** The corpus is reindexed from
  SharePoint; existing `kb-chunks` content is discarded.

## Multi-team users

A user may belong to several teams — the working example is a user in both Star
and HR. For a user's *own* teams this is not an access-control question: they
are entitled to both corpora and could switch and re-ask. The decision is about
answer quality and traceability.

**Decision: one team per chat.** The user selects a team when starting a chat;
it is fixed for that chat's lifetime, and the sidebar shows only the active
team's chats.

Two alternatives were rejected:

- **Team switchable mid-chat.** Earlier turns remain in the context window
  grounded in one team's documents while later retrieval draws from another,
  so the model blends corpora with no signal to the user about which turn came
  from where.
- **Multi-select, union the results.** A query like "what is our escalation
  process" legitimately matches both corpora, and the model merges two teams'
  distinct processes into one confident answer that is wrong for both. It also
  breaks `k` allocation — a single global `k` over a union lets the larger
  corpus crowd out the smaller, requiring per-team retrieval and a merge step —
  and it destroys per-answer auditability, which matters specifically because
  HR is in scope.

### No-results escape hatch

The real cost of one-team-per-chat is that the user must guess which team owns
the answer before asking, and not knowing where a document lives is the reason
the assistant exists.

When a scoped search returns nothing above a relevance floor, the API runs a
**count-only** probe against the user's other allowed teams and the answer
reports it:

> No Star documentation covers this. You also have access to HR, which has 3
> matching documents — switch to HR and ask there?

No content crosses the team boundary; only a match count for teams the user
already has access to. Grounding stays single-corpus and every answer still
traces to exactly one team.

`Scope` holds a single team, not a set. If true multi-team retrieval is wanted
later, widening it is a contained change precisely because every retrieval path
already funnels through the chokepoint. Building the union machinery now buys
nothing.

## Retrieval transparency

A collapsible right-hand panel shows the retrieved context for the selected
answer.

**It shows what the model actually received.** The prompt is built from the
`RerankTopN` chunks in `chosen`, not from all `TopK`. Displaying any chunk the
model did not see invites the user to attribute a claim to a document that had
no part in producing it, which is worse than showing nothing.

The chunks retrieved but *not* passed to the model are the most useful
diagnostic signal — they reveal when the right document was retrieved and then
cut. Only `chosen` is streamed today, so the orchestrator gains a `retrieval`
event carrying all `TopK` chunks with a `used` boolean. Unused chunks render
below a divider, de-emphasised.

Each chunk displays its page title, section path, similarity score, and team
badge. Scores expose the case where nothing relevant was found and the model
improvised. The team badge is a continuous visual check that the segregation
filter is working.

The panel serves two audiences with opposite needs: builders want scores,
dropped chunks and provenance detail; an end user asking about leave policy
wants a title and a link. One component, two densities — expanded by default
when a debug flag is set (`KA_LLM_MODE=mock` implies it), collapsed otherwise.
The existing inline citations in `Message.jsx` remain as always-visible
provenance regardless.

## Architecture

### Team resolution

New package `internal/team`:

- `Team` — a validated slug type. Only slugs present in the configured registry
  are constructible; an unknown slug is an error, never a pass-through.
- `Registry` — loaded from config, mapping team slug to display name and (for
  ingestion) SharePoint site ID.
- `Resolver` interface: `AllowedTeams(ctx context.Context) ([]Team, error)`.

Two implementations:

- `StaticResolver` — demo. A hardcoded user-to-teams map, keyed off the
  existing dev header. Ships enabled in dev mode only.
- `EntraResolver` — production. Reads group claims from the verified JWT and
  maps Entra group object IDs to teams via config. Deferred, but the interface
  and the call sites are built now so adding it touches one file.

This mirrors the dev/prod split already present in
`services/chat-api/internal/middleware/auth.go`.

### Scope: the enforcement chokepoint

`rag.Scope` carries the resolved team and the caller's ACL groups. Its fields
are **unexported**, and it is constructible only through a constructor in the
auth/middleware layer that takes resolved identity. A handler cannot build a
`Scope` from a request body.

The `rag.Retriever` interface changes:

```go
Search(ctx context.Context, query string, scope Scope, k int) ([]Chunk, error)
```

replacing the current `groups []string` parameter. Every implementation —
`opensearch.Client`, `opensearch.MemoryStore`, and the session retriever — must
be updated, so the compiler proves that no retrieval path bypasses team
scoping. This is the central design decision: enforcement is structural, not
conventional.

Request flow:

1. Middleware verifies identity and calls `Resolver.AllowedTeams`.
2. The handler reads the client's requested team from the request body and
   **intersects** it with the allowed set. A team outside the set is rejected
   with 403 — never silently substituted, which would mask both bugs and
   probing.
3. The middleware constructor produces a `Scope` from the intersected result.
4. The `Scope` is passed to the orchestrator and on to every retriever.

The dropdown narrows; it never grants.

### Retrieval query

`opensearch.Client.Search` moves from a `post_filter` to a filter inside the
`knn` clause, supported by the `lucene` engine already configured in
`internal/index/ensure.go`.

This is both a correctness and an isolation fix. `post_filter` retrieves the
global top-k across all teams and then discards non-matching hits, so a Coupa
query competing against a larger HR corpus can return two chunks or none, and
non-team documents are scored before being dropped. A filtered kNN applies `k`
within the team partition.

`MemoryStore.Search` applies the equivalent predicate before scoring.

### Index mapping

`internal/index/ensure.go` gains a `team` keyword field. `index.Doc` and
`rag.Chunk` gain a corresponding `Team` field.

`team` and `aclGroups` stay separate. `team` answers *which corpus*; `aclGroups`
answers *who within it may see this*. Overloading one field for both forecloses
any future within-team restriction.

Because the mapping changes, the index is recreated rather than updated;
`EnsureIndex` remains idempotent for the new mapping.

### Ingestion

New `services/ingestion/internal/sharepoint`, a Microsoft Graph client fanning
into the existing internal `page` shape alongside the GitLab and fixtures
sources.

Per-team configuration: site ID, document library, credentials, and a persisted
delta token.

Pipeline, extending what `services/ingestion/cmd/indexer` already does:

```
crawl → PutRaw (S3, keyed by team) → extract (docx/pdf/html)
      → PutNormalized → chunk → embed → index with team
```

Rules:

- **Team is a command-line flag on the ingest run, never inferred from document
  content or path.** One run per team.
- Credentials are scoped per team where possible.
- Graph throttling (429 with `Retry-After`) is handled in the crawler, not the
  indexer.
- Delta tokens are persisted so subsequent runs re-crawl only what changed.

Graph permissions must use **`Sites.Selected`**, not `Sites.Read.All`.
`Sites.Read.All` grants the app read access to every site in the tenant, making
a single team-mapping bug sufficient to expose HR content. `Sites.Selected`
bounds the blast radius at the identity layer, independent of application filter
logic.

Staging in S3 is retained because: re-embedding after chunk-size, model, or
metadata changes must not require a re-crawl; Graph throttling makes re-crawling
slow and unreliable; SharePoint content requires an extraction stage whose input
and output must both be inspectable when an answer is wrong; and delta sync
requires previous state.

### Session uploads

`session.Store` entries become keyed by team as well as session, and uploaded
chunks carry the team stamp (replacing the hardcoded `SpaceKey: "UPLOAD"` in
`services/chat-api/internal/handler/upload.go`). An upload made while scoped to
one team must not surface while scoped to another.

### Chat persistence

`chats` gains a `team` column, `NOT NULL`, and the user-scoped index becomes
`(user_id, team, updated_at DESC)`. The chat-list query filters on the resolved
team so a conversation held under one team does not appear in another team's
sidebar. A chat's team is fixed at creation and never changes.

### UI

- New `GET /api/teams` returns only the caller's allowed teams. The list is not
  hardcoded in React.
- `TopBar` gains the team selector. Selection is required before the composer
  is enabled.
- The selected team is sent with every chat, upload, and chat-list request.
- Switching teams starts a new chat rather than re-scoping the current one,
  since a chat's team is fixed at creation.
- A new collapsible context panel renders the `retrieval` event, as described
  in *Retrieval transparency*. `Message.jsx` keeps its inline citations
  unchanged.

## Testing

The tests that make the boundary meaningful:

1. **Adversarial retrieval.** Using the mock embedder, construct a corpus where
   the nearest neighbors to a Coupa query are HR chunks. Assert a Coupa-scoped
   search returns none of them. Without this, a regression to `post_filter`
   passes every ordinary query.
2. **Forged team rejected.** A request naming a team outside the caller's
   allowed set returns 403 and performs no retrieval.
3. **Filtered kNN returns full k.** With a team's corpus larger than `k` and
   other teams larger still, a scoped search returns `k` chunks — the
   regression test for the `post_filter` starvation bug.
4. **Chat and upload isolation.** Chats and session uploads created under one
   team are absent from another team's list and retrieval.
5. **Ingestion stamping.** Every chunk produced by a run carries the run's team;
   no chunk is indexed without one.
6. **Panel matches the prompt.** The chunks marked `used` in the `retrieval`
   event are exactly the chunks `BuildPrompt` received — asserted directly, so
   the panel cannot drift from the prompt.
7. **Escape hatch counts only.** A no-results query returns match counts for
   other allowed teams and no chunk text or titles from them, and reports
   nothing for teams the user is not in.

## Consequences

- The `Retriever` interface change touches every implementation and their call
  sites. This is intentional and is the mechanism by which enforcement holds.
- The index is recreated and the corpus reindexed from SharePoint.
- The GitLab connector remains but is no longer the primary source.
- Within-team confidentiality is not addressed; `aclGroups` is reserved for it.
- Multi-team users trade one-click cross-team search for single-corpus
  grounding and per-answer auditability; the escape hatch mitigates the cost.
- The transparency panel makes retrieval quality visible, including the absence
  of real reranking.
