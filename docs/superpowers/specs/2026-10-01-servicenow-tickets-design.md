# ServiceNow Real-Time Tickets — Design

Date: 2026-10-01
Status: Design (approved for spec review)

## Goal

Let a user ask, in natural language, for their team's open ServiceNow tickets
(e.g. "latest open ticket for Team star") and get back a short, LLM-written
summary of **real-time** tickets fetched live from ServiceNow — never from the
document corpus.

Success: a ticket-intent turn returns a summary of actual open incidents for the
caller's team, or an honest status when there are none / ServiceNow is
unreachable. It must never answer a ticket question from RAG documents.

## Non-goals (deliberately skipped)

- OAuth (basic auth only for now; see Auth).
- Ticket creation or update — read-only.
- Pagination / rich filtering beyond "latest N open".
- Caching ServiceNow responses.
- Wiring the per-team contact into the summary (field is stored, not surfaced yet).

## Architecture

A ticket turn is a **second branch** inside `rag.Orchestrator.Answer`, placed
right after the existing greeting gate and running *instead of* RAG retrieval.
The branch point is the orchestrator, not the HTTP handler, because that is
where retrieval, rewrite and the greeting gate already live.

```
request (X-Team=star, "latest open ticket for star")
  → Orchestrator.Answer
      → greeting gate (unchanged)
      → INTENT GATE (new, fires on every turn incl. first):
            heuristic fast-path (INC\d+, "ticket", "incident", "open ticket")
            → no match AND classifier configured → one LLM classify call
                                       │
           intent == "ticket" ? ───────┤
                                       │
   yes → Tickets.LatestOpen(team)      no → existing RAG path (unchanged)
          → feed tickets to the LLM answer step → short summary
          → empty / error / not-configured → honest status, NO RAG fallback
```

### Why a dedicated gate, not the rewrite step

The rewrite step (`orchestrator.go` `queries`) is skipped on a first turn with
no history when dual retrieval is off (prod default `KA_RETRIEVE_MODE=single`).
A ticket query is usually a first turn, so folding intent into rewrite would
never classify the common case. The gate therefore mirrors `isGreeting`: it runs
before retrieval on every turn.

### Invariant: team is never inferred from text

Team comes from the `X-Team` header and `team.Authorize`, never from the query
text. "for Team star" in the message is ignored for scoping; the ServiceNow
query is scoped to the caller's authorized team. A request mentioning a team the
caller cannot access is already refused by `team.Authorize` — unchanged.

## Components

### `internal/servicenow` (new)

Thin, read-only client.

- `type Incident struct { Number, ShortDescription, State, Priority, UpdatedOn, URL string }`
- `LatestOpen(ctx, group string, limit int) ([]Incident, error)`

Hits the Table API:

```
GET /api/now/table/incident
  ?sysparm_query=assignment_group.name=<group>^active=true^ORDERBYDESC=sys_updated_on
  &sysparm_limit=<limit>
  &sysparm_display_value=true
```

Errors wrapped `fmt.Errorf("servicenow: ...: %w", err)`; operator-facing messages
say how to fix. Tested with an `httptest` server (success / empty / 500 / timeout).

### `internal/team` — config-driven workgroup + contact

Extend `Info`:

```go
type Info struct {
    Slug            string
    DisplayName     string
    SiteID          string
    ServiceNowGroup string // ServiceNow assignment_group ("workgroup"); "" disables tickets for this team
    Contact         string // on-call / owning contact; stored, not yet surfaced
}
```

`ServiceNowGroup` and `Contact` are **config-driven, not literals**: a workgroup
can change without a rebuild. `DefaultInfos()` keeps only the stable identity
(`Slug`, `DisplayName`); the ticket metadata is overlaid at startup from config.

New in `team`:

```go
type Metadata struct {
    ServiceNowGroup string `json:"serviceNowGroup"`
    Contact         string `json:"contact"`
}
func ParseMetadata(raw string) (map[string]Metadata, error) // slug -> Metadata; "" raw -> empty map
func ApplyMetadata(infos []Info, meta map[string]Metadata) []Info // overlay onto a copy; unknown slugs ignored
```

Source: a single env var `KA_TEAM_METADATA` holding a JSON object keyed by team
slug, e.g. `{"star":{"serviceNowGroup":"Star Support","contact":"star-oncall@example.com"}}`.
One var, works in local `.env` and in the K8s env block (no file mount). The
workgroup is not a secret, so it is plain config in both environments — only the
ServiceNow *password* uses Secrets Manager. Malformed JSON fails fast at startup
(loud), never silently disabling tickets. A team whose slug is absent (or has an
empty `serviceNowGroup`) returns the "not configured" status rather than
querying.

### `internal/rag` — intent gate + ticket branch

Unchanged `rewrite`. Instead the orchestrator grows a gate that mirrors the
greeting gate, plus a decoupled ticket source so `rag` never imports
`servicenow` or `team`-config detail beyond the `team.Team` it already holds.

New in `rag`:

- `type Ticket struct { Number, ShortDescription, State, Priority, UpdatedOn, URL string }`
- `type TicketSource interface { LatestOpen(ctx context.Context, t team.Team, limit int) ([]Ticket, error) }`
  — a sentinel `ErrTeamNotConfigured` distinguishes "no ServiceNow group for
  this team" from a transport error.
- `type Classifier interface { Classify(ctx context.Context, question string) (bool, error) }`
  — returns whether the question is a ticket query. Optional; nil means
  heuristic-only.
- `func isTicketQuery(question string) bool` — the heuristic fast-path
  (`internal/rag/ticket.go`), tested like `isGreeting`.
- `Orchestrator` gains optional fields `Tickets TicketSource`, `Classifier
  Classifier`, and `TicketLimit int` (default 5).

Flow in `Answer`, after the greeting gate: if `isTicketQuery` or (Classifier
set and it returns true), take the ticket branch; otherwise fall through to RAG
unchanged. The branch fetches via `Tickets.LatestOpen`, streams a short summary
built from a ticket prompt, emits empty `retrieval`/`citation` events (as the
greeting gate does), then `done`. No RAG fallback on a ticket turn.

### chat-api wiring — `ticketSource` adapter

A small adapter in the chat-api wiring implements `rag.TicketSource`: it holds
the `team.Registry` and a `*servicenow.Client`, resolves `team.Team` →
`Info.ServiceNowGroup` (empty → `ErrTeamNotConfigured`), calls
`client.LatestOpen(group, limit)`, and maps `servicenow.Incident` → `rag.Ticket`.
This is the only place `servicenow` and `team` config meet `rag`.

### config

Env-driven, same helpers as today:

- `KA_SERVICENOW_MODE` — `off` (default) / `on`. Gates the whole feature.
- `KA_SERVICENOW_URL` — instance base URL.
- `KA_SERVICENOW_USER` — integration user id.
- `KA_SERVICENOW_SECRET_ID` — Secrets Manager secret id holding the password (AWS).
- `KA_SERVICENOW_PASSWORD` — password for local dev only.
- `KA_SERVICENOW_CLASSIFY` — `off` (default) / `on`. Enables the LLM intent
  classifier fallback; off = heuristic-only gate.
- `KA_SERVICENOW_LIMIT` — max tickets fetched/summarized (default 5).
- `KA_TEAM_METADATA` — JSON object, team slug → `{serviceNowGroup, contact}`.
  Plain config in both local and AWS (not a secret). Empty = no team has a
  workgroup, so ticket lookup reports "not configured" for every team.

**Credential source split:**

- **Local dev:** instance URL, user id, and password all live in `.env`
  (`KA_SERVICENOW_URL`, `KA_SERVICENOW_USER`, `KA_SERVICENOW_PASSWORD`).
- **AWS:** URL and user id come from config/env; the **password comes from AWS
  Secrets Manager** via `KA_SERVICENOW_SECRET_ID`, reusing the existing
  `secrets.FetchAPIKey` pattern (same as the Anthropic key). `KA_SERVICENOW_PASSWORD`
  is not set in AWS.

Resolution order for the password: if `KA_SERVICENOW_SECRET_ID` is set, fetch from
Secrets Manager; otherwise use `KA_SERVICENOW_PASSWORD`.

## Intent detection

A dedicated gate in the orchestrator, before retrieval, on every turn:

- **Heuristic fast-path** (`isTicketQuery`, always on): case-insensitive match on
  `INC\d+`, or the words "ticket" / "incident" / "open ticket". Deterministic,
  cheap, no LLM. Handles the common phrasing.
- **LLM classifier** (optional `Classifier`): runs only when the heuristic does
  *not* match and `KA_SERVICENOW_CLASSIFY` is on. One cheap classify call catches
  phrasing the heuristic misses ("any outages for star?"). It is a real extra
  LLM call on non-heuristic turns — accepted for robustness. When unset, the gate
  is heuristic-only.

The gate decides *intent only*, never *which team* — team stays from `X-Team` +
`Authorize`.

## Auth

Basic auth (a dedicated ServiceNow integration user) against the REST Table API.
OAuth is out of scope; if a later prod security review requires it, it is a
single-spot change inside `internal/servicenow`.

## Error / empty handling

Ticket intent short-circuits RAG entirely.

- Empty result → `"No open tickets for <team display name>."`
- Error / timeout → `"Can't reach ServiceNow right now — try again in a moment."`
- Team has no `ServiceNowGroup` configured → `"Ticket lookup isn't configured for <team>."`

The LLM only ever sees real fetched tickets, so it cannot fabricate ticket
details.

## Testing

- `servicenow`: `httptest` stub — success, empty, 500, timeout; query/auth shape.
- `team`: `ParseMetadata` (valid JSON, empty string, malformed → error) and
  `ApplyMetadata` (overlay by slug, unknown slug ignored, empty map leaves
  groups empty).
- `rag`: `isTicketQuery` heuristic table test; orchestrator ticket-branch tests
  (success summary, empty status, transport error status, not-configured status)
  using stub `TicketSource`/`Classifier`/`LLM`; confirms no retrieval call on a
  ticket turn.
- chat-api `ticketSource` adapter: team→group resolution, empty group →
  `ErrTeamNotConfigured`, `Incident`→`Ticket` mapping.
