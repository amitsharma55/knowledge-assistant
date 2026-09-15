---
url: https://sf-demo.coupahost.com/coupapay/virtual-cards
updated: 2026-08-20
---
# Coupa Pay Virtual Cards

Coupa Pay can issue a single-use virtual card number for an approved purchase
order, so a supplier that accepts cards is paid without an ACH setup.

## Issuance

- When a PO is approved for a card-accepting supplier, Coupa Pay issues a
  single-use virtual card scoped to the PO amount and a 30-day validity window.
- The card number is revealed to the supplier through the Supplier Portal, never
  emailed.

## Reconciliation

Each virtual card settles against exactly one PO, so the settlement file
reconciles one-to-one with the originating purchase order.
