---
url: https://sf-demo.coupahost.com/middleware/requisition/approval-matrix
updated: 2026-07-15
---
# Requisition Approval Matrix

How requisitions route for approval based on amount, commodity, and cost center.

## Rules

- Amount thresholds add approver levels; a capital commodity always adds the
  asset manager regardless of amount.

## Notes

The matrix is evaluated at submission; a later change to the matrix does not
re-route an already-approved requisition.
