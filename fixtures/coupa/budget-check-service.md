---
url: https://sf-demo.coupahost.com/middleware/budget/check-service
updated: 2026-08-23
---
# Budget Check Service

A synchronous service Coupa calls at requisition submission to confirm budget
is available before routing for approval. Like other synchronous services it
holds no data of its own.

## Endpoint

- `POST /budget/check` — returns `available`, `committed`, and a decision of
  `pass`, `warn`, or `block` for a cost center and period.

## Failure modes

- `BUDGET-PERIOD-MISSING` — no budget is loaded for the requested period; the
  service returns `warn` so submission is not hard-blocked on a data gap.
