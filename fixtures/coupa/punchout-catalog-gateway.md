---
url: https://sf-demo.coupahost.com/middleware/punchout/catalog-gateway
updated: 2026-07-21
---
# Punchout Catalog Gateway

The Punchout Catalog Gateway brokers cXML punchout sessions between Coupa and
external supplier catalog sites. It is session-oriented and holds no product
data of its own — the ServiceNow product sync is what moves catalog items.

## Flow

- Coupa opens a `PunchOutSetupRequest`; the gateway signs it and redirects the
  user to the supplier site.
- The supplier returns a `PunchOutOrderMessage` cart, which the gateway maps
  back to Coupa requisition lines.

## Failure modes

- `PUNCH-AUTH-REJECT` — the supplier rejected the shared secret; rotate
  `punchout/<supplier>` in Secrets Manager.
- `PUNCH-CART-EMPTY` — the returned cart had no lines; usually the user
  abandoned the session.
