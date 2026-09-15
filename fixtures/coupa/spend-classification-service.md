---
url: https://sf-demo.coupahost.com/middleware/spend/classification
updated: 2026-07-19
---
# Spend Classification Service

Assigns a commodity code to each requisition and invoice line by classifying
its description against the taxonomy. Synchronous at line entry.

## Behavior

- Returns a `commodity_code`, a confidence, and the taxonomy version used.
- Low-confidence lines are left unclassified for buyer review rather than
  guessed.
