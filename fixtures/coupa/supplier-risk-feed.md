---
url: https://sf-demo.coupahost.com/middleware/supplier/risk-feed
updated: 2026-08-15
---
# Supplier Risk Feed

Ingests supplier risk scores from the external risk provider and stamps them on
the Coupa supplier record so sourcing can factor risk into awards.

## Fields

- `coupa_supplier_id`, `financial_risk_score`, `sanctions_flag`,
  `last_assessed_at`.

## Failure modes

- `RISK-SUPPLIER-UNKNOWN` — the provider returned a score for a supplier not in
  Coupa; the record is skipped and logged, not created.
