---
url: https://sf-demo.coupahost.com/middleware/approval-delegation
updated: 2026-08-04
---
# Approval Delegation

How approvers delegate authority while away.

## Behavior

- A delegation names a delegate and a date range.
- It applies only to approvals, not to editing documents.

## Failure modes

- `DELEG-SELF` — a user cannot delegate to themselves.
