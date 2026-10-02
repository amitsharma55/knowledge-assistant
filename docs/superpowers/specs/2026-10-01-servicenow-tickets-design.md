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

A ticket turn is a **second branch** in the chat handler that runs *instead of*
RAG retrieval, not alongside it.

```
request (X-Team=star, "latest open ticket for star")
  → rewrite step also emits intent  ──┐  (+ heuristic fast-path: INC\d+, "open ticket")
                                       │
           intent == "ticket" ? ───────┤
                                       │
   yes → servicenow.LatestOpen(team's group)   no → existing RAG path (unchanged)
          → feed tickets to existing LLM answer step → short summary
          → empty/error → honest status, NO RAG fallback
```

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

### `internal/team`

Extend `Info`:

```go
type Info struct {
    Slug            string
    DisplayName     string
    SiteID          string
    ServiceNowGroup string // ServiceNow assignment_group name; "" disables tickets for this team
    Contact         string // on-call / owning contact; stored, not yet surfaced
}
```

Populate in `DefaultInfos()` literals (team config is not Terraform, so literals
are consistent with today). A team with an empty `ServiceNowGroup` returns the
"no tickets configured" status rather than querying.

### `internal/rewrite`

Add an `intent` field to the parsed JSON in `prompt.go`; both `Ollama` and
`Bedrock` backends return it. Signature grows to carry intent alongside the
rewritten query; both callers updated. Allowed values: `ticket`, `docs`,
`greeting` (default `docs` on parse miss).

### chat handler

- Determine intent (heuristic fast-path, then rewrite's `intent`).
- On `ticket`: resolve the caller's team → `ServiceNowGroup` → `servicenow.LatestOpen`
  → reuse the existing answer-generation LLM call with tickets as context (in
  place of chunks) → short summary.
- Honest status for empty / error (see below). No RAG fallback on a ticket turn.

### config

Env-driven, same helpers as today:

- `KA_SERVICENOW_MODE` — `off` (default) / `on`. Gates the whole feature.
- `KA_SERVICENOW_URL` — instance base URL.
- `KA_SERVICENOW_USER` — integration user id.
- `KA_SERVICENOW_SECRET_ID` — Secrets Manager secret id holding the password (AWS).
- `KA_SERVICENOW_PASSWORD` — password for local dev only.

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

- **Heuristic fast-path** (always on, even when rewrite is off): `INC\d+` or
  literal "open ticket" / "incident" → `ticket`. Deterministic, cheap.
- **LLM classifier**: when rewrite is on, the rewrite call returns `intent`
  alongside the rewritten query — no extra round-trip. When rewrite is off, only
  the heuristic runs (obvious cases only; acceptable).

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
- `team`: new fields present and parsed.
- `rewrite`: parse test for the `intent` field (both backends' parse paths).
- chat handler: ticket branch success, authorization refusal, empty, and error
  messages; confirms no RAG on a ticket turn.
