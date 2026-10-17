# ServiceNow Ticket Creation — Design

Date: 2026-10-16
Status: Design (approved for spec review)

## Goal

Let a user file a ServiceNow incident for their team from chat, in natural
language, through a confirm-before-write conversation: the assistant collects
the issue and a severity, shows a draft, and files the incident only after an
explicit confirmation. Extends the read-only ticket feature
([2026-10-01-servicenow-tickets-design.md](2026-10-01-servicenow-tickets-design.md)),
which stays unchanged.

Success: "create a ticket for my group" walks the user through issue → severity
→ draft → confirm, then returns the real `INC` number and a link to the created
incident, scoped to the caller's team. A misparse, a cancel, or a stray reply
never files an incident.

## Non-goals (deliberately skipped)

- Updating or closing tickets (create only).
- Fields beyond `short_description`, `assignment_group`, `priority` (and the
  caller). No category, CI, description, attachments.
- An LLM in the create path. Create collects two fields and files them; the
  draft and prompts are templates. Keeping it deterministic removes any chance
  of a fabricated field reaching a write, and makes the write testable.
- Server-side draft state. KA runs several chat-api replicas behind one ALB, so
  in-memory per-request state would not survive the next turn landing on a
  different replica. The conversation history — already threaded into
  `Orchestrator.Answer` — is the only state.
- New config. Create is live whenever `KA_SERVICENOW_MODE=on`.

## Architecture (Approach C: history-derived, deterministic confirm)

A third branch in `rag.Orchestrator.Answer`, placed **before** the greeting gate
and the read-ticket branch. It fires when the turn is a create request **or** a
create is already in progress:

```
request (X-Team=star, message, history)
  → Orchestrator.Answer
      → CREATE GATE (new): Tickets != nil AND
            (isCreateTicketQuery(message) OR createInProgress(history))
          │
         yes → answerCreateTicket  (deterministic state machine, below)
          │
         no  → greeting gate (unchanged)
             → read-ticket branch (unchanged)
             → RAG (unchanged)
```

The create gate is first because the follow-up answers ("the email server is
down", "high", "confirm") contain none of the create keywords; only
`createInProgress(history)` routes them back into the branch. It is checked
before the read heuristic so "create a ticket" is not swallowed by read's
`ticket`/`incident` match.

### Invariant: team is never inferred from text

As with read, the `assignment_group` is the caller's team workgroup
(`X-Team` → `team.Info.ServiceNowGroup`), never parsed from the message. "for
the DBA team" in the text is ignored for scoping.

## Intent detection

Both heuristics live in `internal/rag/ticket.go`, tested like `isTicketQuery`:

- `isCreateTicketQuery(msg)` — true when `msg` contains a create verb
  (`create`, `open`, `raise`, `file`, `log`, `submit`, `new`) **and** `ticket`
  or `incident`. Deterministic; no LLM (a write must not fire on model
  judgement).
- `createInProgress(history)` — true when the last assistant turn is one of the
  branch's known prompts (issue prompt, severity prompt, or a draft). This is
  what carries turns 2–4 (the slot answers and the confirm) into the branch.

## The state machine

`answerCreateTicket` derives the current phase from `(history, message)` each
turn — there is no stored state. Phases:

1. **Fresh request.** `isCreateTicketQuery(message)` and not in progress. If the
   message carries an inline issue (`create a ticket: email server down`), keep
   it and go to severity; else emit the **issue prompt** and stop.
2. **Issue reply.** Last assistant turn is the issue prompt → this whole message
   is the `short_description`. Emit the **severity prompt** and stop.
3. **Severity reply.** Last assistant turn is the severity prompt → map the word
   to a ServiceNow priority (`critical`→1, `high`→2, `moderate`/`medium`→3,
   `low`→4). An unmapped word re-emits the severity prompt (does not advance).
   On success, emit the **draft**: the issue, the team's workgroup, and the
   mapped priority, ending with "Reply **confirm** to file it, or **cancel**."
4. **Confirm reply.** Last assistant turn is a draft → if the message is
   affirmative (`confirm`/`yes`/`y`), reconstruct `(issue, priority)` from
   history and call `Tickets.Create`; reply "Filed `INC…` — <url>". If it is
   `cancel`/`no`, reply "Cancelled — nothing was filed." Anything else re-emits
   the draft.

The exact prompt strings are constants; phase detection matches the last
assistant turn against them. This is a documented heuristic ceiling: a user who
pastes the literal prompt text back would confuse detection. Acceptable for the
demo; the upgrade path is an invisible per-message marker if it ever matters.

**Idempotency.** The fields are read from the two user replies in history, not
from stored state. After a successful file, the assistant turn is "Filed
`INC…`", which matches no draft prompt, so a later stray "yes" starts a fresh
request rather than filing again.

### Prompts and draft (constants)

- Issue: `What's the issue you'd like to report? Describe it in one line.`
- Severity: `How severe is it? Reply Critical, High, Moderate, or Low.`
- Draft: starts with a fixed prefix (`Draft incident for `) so it is
  recognizable, lists issue / group / priority, and ends with the confirm line.

## Components

### `internal/servicenow` — add `Create`

```go
func (c Client) Create(ctx context.Context, group, shortDescription, priority, caller string) (Incident, error)
```

POSTs to `/api/now/table/incident?sysparm_input_display_value=true` with a JSON
body `{short_description, assignment_group, priority, caller_id?}`.
`sysparm_input_display_value=true` lets `assignment_group` go by name and
`priority` by its number, matching the read query's display convention. `caller`
is best-effort (omitted if empty). Non-2xx is wrapped like the GET, with an
operator-facing hint. Returns the created incident (number + `sys_id` → URL),
decoded from the POST response `result`.

### `internal/rag` — create types, gate, state helpers, branch

- `type TicketDraft struct { ShortDescription, Priority string }`
- `TicketSource` gains `Create(ctx context.Context, t team.Team, d TicketDraft) (Ticket, error)`.
- `isCreateTicketQuery(msg) bool`, `createInProgress(history []Turn) bool`,
  and `draftFromHistory(history []Turn, message string) (phase, issue, priority string)`
  (pure, table-tested).
- `Orchestrator.answerCreateTicket(ctx, message, history, scope, out)` runs the
  machine, streaming empty `retrieval`/`citation` events (like the other
  branches), the phase's text as a `token`, then `done`. The write happens only
  in the confirm phase.

Priority mapping (`critical`→`1`, `high`→`2`, `moderate`/`medium`→`3`,
`low`→`4`) is a small helper in `ticket.go`.

### chat-api adapter — add `Create`

`ticketSource.Create` resolves the team's `ServiceNowGroup` (empty →
`ErrTeamNotConfigured`), calls `client.Create(group, d.ShortDescription,
d.Priority, caller)`, maps `servicenow.Incident` → `rag.Ticket`. The caller is
left empty for now (the dev identity is an email, not a resolvable
`sys_user`); wiring a resolvable caller is a later refinement.

## Auth / permissions

The integration user needs incident **create** rights (the `itil` role or an
equivalent write ACL) in addition to read. This is instance configuration, not
code: a 403 on create surfaces as "Couldn't file the ticket in ServiceNow right
now." Verified once against the dev instance during implementation (the admin
user already has it).

## Error / empty handling

- Team has no workgroup → "Ticket creation isn't configured for <team>."
- Create transport/non-2xx → "Couldn't file the ticket in ServiceNow right now —
  try again in a moment." No partial state; the draft stays reconstructable from
  history so the user can retry with "confirm".
- Unmapped severity word → re-ask, do not advance.

## Testing

- `rag`: `isCreateTicketQuery` and `createInProgress` table tests;
  `draftFromHistory` across every phase; `answerCreateTicket` walked turn by
  turn (fresh → issue → severity → draft → confirm→create) with a stub
  `TicketSource`, asserting `Create` fires **only** at confirm and **exactly
  once**; plus cancel, unmapped-severity re-ask, not-configured, and transport
  error. A read-phrased turn must still reach the read branch, and a
  create-phrased turn must not.
- `servicenow`: `httptest` POST — asserts method, the decoded body
  (`short_description`, `assignment_group`, `priority`), and returns a created
  number; non-2xx → error.
- chat-api adapter: group resolution, empty group → `ErrTeamNotConfigured`,
  `Incident`→`Ticket` mapping, and that the draft's fields reach the client.
