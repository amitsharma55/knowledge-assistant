---
url: https://sf-demo.coupahost.com/middleware/gl/export-bridge
updated: 2026-09-02
---
# GL Export Bridge

The GL Export Bridge posts approved Coupa invoice and expense transactions to
the general ledger. It is an outbound batch integration; it never reads from
Coupa synchronously the way AVR does.

## Schedule

- `gl-export-nightly` runs at 02:00 and posts the prior day's approved
  transactions as journal entries.

## Fields

- `journal_id`, `cost_center`, `gl_account`, `debit`, `credit`, `currency`,
  `source_document_id`.

## Failure modes

- `GL-UNBALANCED-BATCH` — debits and credits do not net to zero; the whole
  batch is rejected rather than posting a partial journal.
- `GL-CLOSED-PERIOD` — the target accounting period is closed; hold the batch
  for the next open period.
