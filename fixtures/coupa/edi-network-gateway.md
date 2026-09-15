---
url: https://sf-demo.coupahost.com/middleware/edi/network-gateway
updated: 2026-08-02
---
# EDI Network Gateway

Brokers EDI documents between Coupa and suppliers on the value-added network.
Translates between Coupa's canonical format and X12 transaction sets.

## Supported sets

- 850 purchase order, 855 acknowledgement, 810 invoice, 856 advance ship notice.

## Failure modes

- `EDI-997-REJECT` — the trading partner returned a negative functional
  acknowledgement; the document is quarantined for EDI-team review.
