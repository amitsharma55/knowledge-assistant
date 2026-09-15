---
url: https://sf-demo.coupahost.com/middleware/tax/facade
updated: 2026-07-29
---
# Tax Engine Facade

The Tax Engine Facade is the Coupa AWS Middleware component that calls the
external tax determination engine when Coupa prices a requisition line. Like
AVR it is synchronous, but it calls out to the tax engine rather than reading
from an on-prem source.

## Endpoints

- `POST /tax/determine` — returns `tax_code`, `jurisdiction`, `rate`, and
  `tax_amount` for a line.
- `POST /tax/validate-exemption` — checks an exemption certificate id.

## Failure modes

- `TAX-RATE-STALE` — the jurisdiction rate table is older than 24h; the facade
  serves the last good rate and flags the line for review.
- `TAX-ENGINE-TIMEOUT` — the tax engine did not answer within 8 seconds.

The facade never writes to PeopleSoft and holds no invoice data; do not confuse
a `TAX-ENGINE-TIMEOUT` with an `AVR-TIMEOUT`, which is a different integration.
