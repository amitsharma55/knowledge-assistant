---
url: https://sf-demo.coupahost.com/middleware/supplier-notifications
updated: 2026-08-09
---
# Supplier Actionable Notifications

How suppliers act on purchase orders directly from email.

## Behavior

- Actionable emails let a supplier acknowledge a PO without logging in.
- The action is signed with a one-time token.

## Failure modes

- `SAN-TOKEN-EXPIRED` — the one-time token expired; the supplier must use the
  Supplier Portal instead.
