---
url: https://sf-demo.coupahost.com/middleware/rcv/bridge
updated: 2026-07-17
---
# Receiving Events Bridge

The Receiving Events Bridge streams goods-receipt events from the warehouse
system into Coupa so that three-way match can complete. It is event-driven over
a message queue.

## Flow

- Each physical receipt emits a `receipt_event` with `po_number`,
  `line_number`, `quantity_received`, and `received_at`.
- Coupa matches the event to the open purchase order line.

## Failure modes

- `RCV-NO-OPEN-PO` — the referenced PO line is already fully received or does
  not exist; the event is parked in a review queue.
