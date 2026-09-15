---
url: https://sf-demo.coupahost.com/middleware/inventory/integration
updated: 2026-07-24
---
# Coupa Inventory Integration

Keeps Coupa Inventory stock levels in step with the warehouse management system
so requisitions can draw from on-hand stock before raising a purchase order.

## Sync

- On-hand quantities sync from the WMS every hour.
- A cycle count adjustment in the WMS posts to Coupa as an inventory transaction
  with the count variance.

## Draw-down

A requisition for a stocked item consumes on-hand inventory first and only
raises a PO for the shortfall.
