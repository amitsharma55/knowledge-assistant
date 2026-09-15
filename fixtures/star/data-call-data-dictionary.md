---
url: https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0020061
updated: 2026-07-31
---
# Data Call Data Dictionary

The common field catalog shared across every state package. State-specific
elements are documented in each state's own overview.

## Common fields

- `policy_number`, `effective_date`, `written_premium`, `earned_premium`,
  `exposure_count`, `county code (FIPS)`, `line_of_business`.

## Conventions

Currency fields are whole dollars. Dates are ISO 8601. A null exposure count is
rejected at validation; zero is a valid value and must be sent explicitly.
