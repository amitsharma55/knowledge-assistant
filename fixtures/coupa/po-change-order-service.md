---
url: https://sf-demo.coupahost.com/middleware/po/change-order
updated: 2026-08-14
---
# PO Change Order Service

Handles amendments to issued purchase orders and retransmits the revised order
to the supplier.

## Rules

- A quantity or price change re-opens approval if it crosses the tolerance.
- A closed PO cannot be changed; a new PO is required instead.

## Failure modes

- `CO-PO-CLOSED` — the target PO is closed; the change is rejected.
