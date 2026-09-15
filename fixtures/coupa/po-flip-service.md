---
url: https://sf-demo.coupahost.com/middleware/po/flip-service
updated: 2026-07-30
---
# PO Flip Service

Turns an approved purchase order into a supplier-facing order document and
transmits it over the supplier's preferred channel (cXML, EDI, or email PDF).

## Channels

- cXML `OrderRequest` for punchout-enabled suppliers.
- EDI 850 for suppliers on the EDI network.

## Failure modes

- `POFLIP-NO-CHANNEL` — the supplier has no transmission channel configured;
  the order is held until one is set on the supplier record.
