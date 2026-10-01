# Query-log capture: privacy review

Status: **capture code merged, SAFE-OFF.** `KA_QUERYLOG_BUCKET` is unset and no
S3 `PutObject` grant exists, so nothing is written anywhere. This document is the
artifact for the privacy/compliance review that gates turning it on. Do not set
the env or grant the IAM until that review signs off.

## What it is

An optional `rag.QueryLog` sink (`services/chat-api/internal/querylog`) that
writes one JSON object per answered query to S3, so the retrieval eval golden set
can be built from real recall failures (below-floor queries, rephrases) instead
of hand-authored guesses. Wired in `cmd/server/main.go` only when
`KA_QUERYLOG_BUCKET` is set; off for local, fixtures, and any deploy without it.

## The record schema

One object at `querylog/YYYY/MM/DD/<unixnano>-<rand>.json`:

| field            | example                          | source |
|------------------|----------------------------------|--------|
| `ts`             | `2026-10-01T14:05:06Z`           | server clock (UTC) |
| `team`           | `"hr"`                           | request scope |
| `query`          | `"how much parental leave do I get"` | the user's message |
| `rewritten`      | `"..."` (omitted if == query)    | history-resolved search query |
| `belowFloor`     | `true`                           | retrieval found nothing at/above the floor |
| `suggestedTeams` | `["coupa"]` (omitted if none)    | other teams that could answer |

There is **no user id, no session id, no IP, no chunk text, and no answer.**

## Field-by-field identifiability (what the review asked to confirm)

- **`query` is the PII.** Confirmed — this is the sensitive field. An HR query
  ("when does my FMLA leave start", "appeal my performance rating") identifies
  the person by its content regardless of the missing user id. Everything else
  below is secondary to this.
- **`rewritten` can *widen* what's captured — flag.** It is the follow-up
  resolved against conversation history, so it folds earlier turns into one
  string: "when does it start" → "when does my military leave start". It adds no
  new *identifier*, but it can surface context the single query did not. It is
  stored only when it differs from `query`. If the review wants the narrower
  capture, we drop this field; it is not required for golden-set mining (the
  original `query` suffices).
- **`suggestedTeams`** — team slugs only (`coupa`/`star`/`hr`), never counts or
  content. It reveals that the asker has access to another team and that their
  question matched there; that is org-structure metadata, not personal data. No
  new identifiability beyond `query` + `team`.
- **`team` + `ts`** — coarse; identifying only in combination with `query`.

Net: `query` (and, more so, `rewritten`) carry the risk. The id-free record
keeps the data about *what was asked*, not *who asked* — but the review should
decide whether `rewritten` is kept.

## Retention — the gap this review must close

**Current state: there is no object TTL.** The `private-bucket` module expired
only *noncurrent versions*; current objects lived forever. A query-log bucket
built on it would keep user queries indefinitely — not acceptable for this data.

**Fix shipped:** the module now takes `object_expiration_days` (default `0` =
keep forever, so every existing durable bucket is unchanged). The query-log
bucket must set it.

**Proposed window: 30 days.** Long enough to accumulate a few hundred real
queries and mine the below-floor/rephrase set for the golden set; short enough
that PII does not linger. Adjust up or down in the review.

Objects are also encrypted at rest (SSE-S3) and TLS-only via the module baseline.

## Exact activation (held — do not apply until sign-off)

1. Provision the bucket with the retention window baked in:

   ```hcl
   module "querylog_bucket" {
     source                  = "../modules/private-bucket"
     name                    = "<project>-querylog"
     noncurrent_version_days = 7
     object_expiration_days  = 30   # retention window for query PII
   }
   ```

2. Grant the chat-api role `s3:PutObject` on `arn:aws:s3:::<project>-querylog/querylog/*` only.
3. Set `KA_QUERYLOG_BUCKET=<project>-querylog` on the chat-api deployment.

Until all three are done, capture stays off and no queries are stored.
