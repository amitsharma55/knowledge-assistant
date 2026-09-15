---
url: https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0021201
updated: 2026-08-22
---
# Data Call Error Codes

The pipeline-level error codes shared across state runs. State validation
warnings are documented in each state's processing rules.

## Codes

- `DC-EXTRACT-EMPTY` — the source extract returned zero rows; the run aborts
  before transform.
- `DC-CONTROL-MISMATCH` — package totals do not tie to the ledger control
  totals beyond tolerance.
