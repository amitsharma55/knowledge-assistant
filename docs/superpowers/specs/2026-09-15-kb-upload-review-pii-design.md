# KB Upload Review & PII Gate — Design

Date: 2026-09-15
Status: Approved for planning

## Problem

Users can attach a document (PDF, Word, Excel) to a chat, ask questions about
it, and optionally **save it to the knowledge base** so its facts are served to
everyone on their team. Today the `persist=true` path in
`services/chat-api/internal/handler/upload.go` chunks, embeds, and writes the
document **straight into the shared OpenSearch index** with no scan and no human
approval. That is unacceptable for a KB that other people trust:

1. A document may contain sensitive information (PI / PII / SPI — SSNs, card
   numbers, bank details, etc.) that must never be indexed.
2. Anything that becomes team-wide knowledge should be **reviewed by a
   human/admin** before it goes live.

This design intercepts the persist path with a **deterministic PII gate** and a
**human review queue**, so a document only reaches the index after passing the
scan and being approved by an admin.

## Scope

In scope:
- Office-format extraction: add `.docx` and `.xlsx` to the existing
  PDF/Markdown/TXT extractor.
- A deterministic PII/SPI detector (`internal/pii`) behind an interface.
- A pending-review queue (`internal/review`) behind an interface, with an
  in-memory demo implementation.
- Rewiring `persist=true` to scan-then-enqueue instead of index-inline.
- An admin review surface (API + minimal UI) to approve/reject.
- A UI warning on the attach control and handling of the new responses.

Out of scope (explicitly deferred):
- Real authz for the admin surface (demo uses an env allowlist — see Risks).
- LLM/Comprehend PII detection (interface is the seam; not built now).
- S3-backed durable review queue (interface is the seam; demo is in-memory).
- OCR for scanned PDFs (already out of scope in `extract`).
- High-quality spreadsheet chunking (row-flattening only — see Risks).

## Decisions (settled during brainstorming)

- **Review model:** demo-grade in-memory queue **now**; S3-quarantine +
  admin-UI is the production evolution, reached by swapping the `review.Store`
  implementation, not rewiring the flow.
- **PII detection:** deterministic regex detectors **now**, behind a
  `pii.Detector` interface so a Bedrock or AWS Comprehend detector drops in
  later without touching callers.
- **On detection:** **high-severity** findings (SSN, credit card, bank/routing,
  DOB) **hard-block** the upload — it returns `422`, tells the user to redact
  and re-upload, and never enters the queue. **Low-severity** signals (email,
  phone) do not block but are **recorded on the pending item** so the reviewer
  sees them.
- **User warning:** the attach control carries a standing warning not to upload
  sensitive information.
- Session-scoped upload (attach + ask questions, no persistence) is **unchanged**.

## End-to-end flow

```
User attaches file (UI warning: "Don't upload sensitive info")
        │
        ▼
POST /v1/upload  (extract → .pdf/.docx/.xlsx/.md/.txt → text)
        │
        ├── session mode (existing): chunks into chat session, ask questions.  UNCHANGED
        │
        └── persist=true (CHANGED — no longer indexes directly):
                │
                ▼
          PII scan (pii.Detector, regex)
                │
      high-severity finding? ──yes──► 422 "Contains SSN/card #…; redact & re-upload"  (never queued)
                │ no
                ▼
          Enqueue as PENDING in review.Store
          (extracted text + low-sev findings + uploader + team)
                │
                ▼
     ── admin reviews (GET/POST /v1/admin/pending) ──
        approve ► ingest.Ingest → index (+ S3 in prod)   reject ► discard
```

The load-bearing change: `persist=true` **stops calling `ingest.Ingest`
inline**. Promotion to the index happens **only** from the admin approve
endpoint.

## Components

### New — `internal/pii` (deterministic detector)

```go
type Severity int   // Low, High
const (
    Low Severity = iota
    High
)

type Finding struct {
    Category string // "ssn", "credit_card", "bank_account", "dob", "email", "phone"
    Severity Severity
    Excerpt  string // short masked snippet for the reviewer, e.g. "•••-••-1234"
    Offset   int    // byte offset in the scanned text
}

type Detector interface {
    Scan(text string) []Finding
}
```

- `RegexDetector` — the demo implementation. Table-driven detectors:
  - **High:** SSN, credit card (regex **+ Luhn check** to suppress false
    positives), bank account / routing number, date of birth.
  - **Low:** email, phone.
- Pure, no external calls, no cost, fully unit-tested (positive + negative
  cases, Luhn edge cases).
- `func HasHigh(findings []Finding) bool` for the block decision.
- The `Detector` interface is the seam: a `BedrockDetector` /
  `ComprehendDetector` drops in later without changing callers.

### New — `internal/review` (pending queue behind an interface)

```go
type Status string // "pending", "approved", "rejected"
const (
    Pending  Status = "pending"
    Approved Status = "approved"
    Rejected Status = "rejected"
)

type Item struct {
    ID        string
    Team      string
    Uploader  string
    Filename  string
    Text      string        // extracted text, ready to ingest on approval
    Findings  []pii.Finding // low-severity only (high-sev never reaches here)
    Status    Status
    Bytes     int
    CreatedAt time.Time
}

type Store interface {
    Enqueue(ctx context.Context, it Item) (string, error) // returns review id
    List(ctx context.Context, team string) ([]Item, error) // team-scoped, pending only
    Get(ctx context.Context, id string) (Item, error)
    SetStatus(ctx context.Context, id string, s Status) error
}
```

- **Demo impl `MemoryStore`** — mutex-guarded map. Zero dependencies. Matches
  the "demo-grade now" decision. Not durable; contents are lost on restart
  (acceptable — see Risks).
- **Prod impl `S3Store` (later, not built now)** — writes
  `pending/<team>/<id>.json` and promotes to `live/<team>/…` on approval. Same
  interface, so the flow code is unchanged. This is the option-1 seam.
- `List` is **team-scoped** — an admin sees only pending items for their team,
  consistent with the KA rule that team is never inferred and never crossed.

### Changed — `internal/extract`

Add two formats to `FromBytes` / `Supported`:
- `.docx`: unzip the OOXML container, read `word/document.xml`, strip tags to
  plain text (small self-contained code, or `github.com/nguyenthenguyen/docx`).
- `.xlsx`: `github.com/xuri/excelize/v2`; flatten each sheet's rows to
  `header: value` lines so a spreadsheet becomes retrievable prose.
- `Supported()` grows to match; the UI accept-list mirrors it.

### Changed — `services/chat-api/internal/handler/upload.go` (`persist=true` branch only)

1. After extraction, `findings := h.Detector.Scan(text)`.
2. `if pii.HasHigh(findings)` → `422 Unprocessable Entity` with body message:
   "This document appears to contain sensitive information (e.g. SSN or card
   number). Remove it and upload again." Nothing is enqueued or indexed.
3. Otherwise `id, _ := h.Review.Enqueue(ctx, review.Item{...})` and respond
   with `{ "status": "pending_review", "reviewId": id, "findings": <low-sev> }`
   instead of `{ "persisted": true }`.
4. The **session-mode branch is untouched.**

New handler fields: `Detector pii.Detector`, `Review review.Store`. The direct
`ingest.Ingest` call is removed from this handler.

### New — `handler/admin.go` + routes

- `GET  /v1/admin/pending` → team-scoped list of pending items (+ findings).
- `POST /v1/admin/pending/{id}/approve` → `ingest.Ingest(item → ingest.Page)`
  into the index, then `SetStatus(id, Approved)`.
- `POST /v1/admin/pending/{id}/reject` → `SetStatus(id, Rejected)`; discard.
- On approve, the `ingest.Page` is reconstructed exactly as the current persist
  path builds it (`SpaceKey: "UPLOAD"`, `URL: upload://<id>/<filename>`, team
  from the item), so indexed output is identical to today's — only gated.

### New — `middleware.RequireAdmin`

- Demo: an env allowlist `KA_ADMIN_USERS` (comma-separated) checked against the
  `X-Dev-User` identity already resolved by `middleware.Auth`. Non-admins → 403.
- Natural prod path: check for an `admin` group in the existing `X-Dev-Groups` /
  Cognito groups already flowing through `Auth`. Noted, not built now.

### Changed — UI

- Composer attach control gains standing warning text: "Don't upload documents
  containing sensitive personal information (SSNs, card numbers, etc.)."
- Accept-list extended to `.docx`, `.xlsx`.
- On `422` sensitive response → inline error asking the user to redact and
  retry.
- On `pending_review` response → confirmation: "Sent for admin review."
- New minimal **Admin panel** (rendered only for an admin identity) listing
  pending items with filename, uploader, low-severity findings, and
  Approve/Reject buttons wired to the admin endpoints.

### Wiring — `services/chat-api/cmd/server/main.go`

Construct `pii.NewRegexDetector()` and `review.NewMemoryStore()`; inject into
`UploadHandler` and the new `AdminHandler`. Register the admin routes behind
`Auth` + `RequireAdmin`.

## Data flow summary

- **Extracted text** is carried on the `review.Item` so approval needs no
  re-upload or re-extraction.
- **High-severity findings** never leave the request handler — they cause a
  `422` and are not stored.
- **Low-severity findings** are stored on the item, masked, for reviewer
  context only; they do not gate anything.
- **Indexing** is byte-identical to the current persist path; the only
  difference is that it is triggered by admin approval rather than by upload.

## Error handling

- Unsupported file type → existing `415` from `extract`.
- Empty/no-text extract (scanned PDF) → existing `422`.
- High-severity PII → `422` with redact message (new).
- `Enqueue` failure → `500`.
- Admin action on a missing/non-pending id → `404` / `409`.
- Approve whose `ingest.Ingest` fails → `502`, item stays `pending` so it can
  be retried (status is only advanced after a successful index).

## Testing

- `internal/pii`: table-driven unit tests — each category positive and
  negative, Luhn true/false for cards, severity assignment, `HasHigh`.
- `internal/review`: `MemoryStore` enqueue/list/get/set, team scoping (an item
  for team A is not listed for team B).
- `internal/extract`: `.docx` and `.xlsx` fixtures → expected text; unsupported
  and empty cases.
- `upload.go` handler (httptest + stub detector/store): high-sev → 422 and
  nothing enqueued; clean → `pending_review` and one item enqueued; session
  mode unchanged (byte-identical chunks).
- `admin.go` handler (httptest + stubs): non-admin → 403; approve indexes via a
  stub indexer and flips status; reject flips status without indexing; ingest
  failure leaves status pending.
- No change to the 51-question retrieval baseline is expected (upload path is
  orthogonal); `/check-retrieval` need only confirm no regression if the
  extractor or chunker is touched.

## Risks & accepted limitations

- **Admin authz is an env allowlist** against a dev header for the demo — not
  real authentication. Accepted for the demo; prod uses Cognito group `admin`.
- **`.xlsx` chunks poorly** — tables flattened to prose contradict the
  "facts as bullets, not tables" fixture guidance. Accepted for the demo;
  proper spreadsheet handling is future work.
- **`MemoryStore` is not durable** — pending items are lost on chat-api
  restart. Accepted; the `S3Store` seam is the durable answer.
- **Regex PII misses contextual sensitivity** (e.g. "employee X earns $Y").
  Accepted; the `Detector` seam admits an LLM/Comprehend detector later.

## Non-goals

This design does not add S3 writes from chat-api, does not change the
session-scoped upload path, does not change retrieval or scoring, and does not
introduce a persistent datastore.
