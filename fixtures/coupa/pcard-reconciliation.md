---
url: https://sf-demo.coupahost.com/middleware/pcard/reconciliation
updated: 2026-07-25
---
# P-Card Reconciliation

Loads daily purchasing-card transaction files from the card network and matches
them to Coupa expense records for reconciliation.

## Schedule

- `pcard-load` runs each morning with the prior day's settled transactions.

## Failure modes

- `PCARD-UNMATCHED` — a transaction has no corresponding Coupa record after
  three days; it is routed to the card administrator for manual coding.
