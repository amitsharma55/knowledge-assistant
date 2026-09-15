---
url: https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0020110
updated: 2026-08-25
---
# Data Call Validation Framework

The common set of pre-submission checks every state package runs before it is
delivered. State-specific rules layer on top of these.

## Universal checks

- Row counts reconcile to the source extract within a 0.1% tolerance.
- Premium sums are non-negative and match the ledger control total.
- Every required common field is present and typed correctly.

## Outcomes

A package failing a hard check is blocked from submission; a soft check
produces a warning the analyst must acknowledge.
