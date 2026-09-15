---
url: https://sf-demo.coupahost.com/middleware/cost-allocation
updated: 2026-07-24
---
# Coupa Cost Allocation Rules

How requisition lines are split across cost centers before they reach the GL
export. This is Coupa-side configuration and involves no middleware integration.

## Split methods

- Percentage split across up to five cost centers.
- Fixed-amount split with a remainder line.

## Validation

A split that does not sum to the line total is rejected at submission, so the
GL export never receives an unbalanced allocation.
