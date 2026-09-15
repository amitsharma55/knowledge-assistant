---
url: https://sf-demo.coupahost.com/middleware/data-retention
updated: 2026-08-05
---
# Data Retention

How long transactional records are kept in Coupa before archival.

## Behavior

- Closed POs and paid invoices are archived after seven years.
- Archived records are read-only.

## Failure modes

- `RETENTION-LEGAL-HOLD` — a record under legal hold is never archived until
  the hold clears.
