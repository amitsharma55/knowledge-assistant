---
url: https://sf-demo.icertis.com/docs/contract-integration/overview
updated: 2026-07-30
---
# Icertis Contract Integration

Moves executed contracts from **Icertis CLM** into **Coupa**, so that
procurement teams can reference contract terms when raising purchase orders and
matching invoices, and then confirms back to Icertis that the load succeeded.
Runs against the Icertis tenant `sf-demo.icertis.com` and the Coupa tenant
`sf-demo.coupahost.com`.

Field-level mappings live in the Icertis field mapping document; recovery
procedures live in the Icertis resync runbook.

## How it works

The integration runs in three phases, in order, for every contract in scope.

1. **Fetch** — the middleware calls the Icertis contract API for contracts
   changed since the last watermark, and stages the response in S3 under
   `s3://sf-coupa-middleware/icertis/staged/`. Nothing is transformed at this
   point, so a fetch can be replayed without re-querying Icertis.
2. **Load** — staged contracts are transformed to the Coupa contract shape and
   posted to the Coupa contracts API. Coupa returns its own contract identifier,
   which is held against the staged record.
3. **Resync** — the middleware calls back to the Icertis API to set the
   integration status on the contract to `LoadedToCoupa`, carrying the Coupa
   contract identifier. Until this confirmation lands, Icertis still shows the
   contract as pending load, even though Coupa already has it.

Each phase is separately retryable. The resync phase is deliberately separate
from the load rather than folded into it: a contract that loaded successfully
must never be re-loaded just because the confirmation failed.

## Sync scope

A contract is fetched when **all** of the following hold:

- `contractStatus` = `Executed` — fully signed
- `contractType` is one of `Master Service Agreement`, `Statement of Work`,
  `Purchase Agreement`, `Amendment`
- `effectiveDate` is on or before today
- `expirationDate` is null, or on or after today

Contracts in `Draft`, `In Review`, `Expired` or `Terminated` are skipped and
counted in the run summary. Amendments propagate as new versions on the same
Coupa contract rather than as separate contracts.

## Jobs

- `icertis-fetch-incremental` — every 15 minutes. Fetches contracts changed
  since the last watermark and stages them.
- `icertis-load` — every 15 minutes, five minutes offset from the fetch. Loads
  staged contracts into Coupa.
- `icertis-resync` — every 15 minutes, ten minutes offset from the fetch.
  Confirms loaded contracts back to Icertis.
- `icertis-resync-retry` — hourly at :40. Re-sends confirmations for contracts
  that loaded but never confirmed.
- `icertis-daily-full` — daily at 01:30 UTC. Full sweep of all in-scope
  contracts, catching anything a missed webhook left behind.

## Volume

- 120–180 contracts fetched per day by `icertis-fetch-incremental`
- The nightly full sweep evaluates roughly 14,000 in-scope contracts and
  typically loads fewer than 200
- 2–5 contracts per day require the resync retry, almost always because Icertis
  was briefly unavailable during the confirmation window
- Failed loads are retried five times with exponential backoff before landing in
  the `icertis-load-dlq` SQS queue

## Changelog

- **2026-06** — Full sweep moved from 02:30 to 01:30 UTC so it completes before
  the Coupa nightly maintenance window.
- **2026-04** — Amendment versioning added. Amendments previously created a
  second Coupa contract; they now propagate as a new version on the same Coupa
  contract, and `contractType` `Amendment` was added to the sync scope.
- **2026-02** — Resync phase separated from the load phase, after a run
  re-loaded contracts whose confirmation had failed and created duplicates in
  Coupa.
- **2025-11** — Initial release covering Master Service Agreements and
  Statements of Work.
