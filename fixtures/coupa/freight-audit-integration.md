---
url: https://sf-demo.coupahost.com/middleware/freight/audit
updated: 2026-08-07
---
# Freight Audit Integration

Sends approved freight invoices to the third-party freight audit provider and
loads back the audited, adjusted amounts.

## Flow

- Nightly, freight invoices are exported to the auditor.
- The auditor returns line adjustments, which post to Coupa as credit or debit
  memos.

## Failure modes

- `FREIGHT-DISPUTE-OPEN` — the auditor flagged a charge the carrier disputes;
  the memo is withheld until the dispute closes.
