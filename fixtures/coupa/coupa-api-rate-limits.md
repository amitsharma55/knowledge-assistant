---
url: https://sf-demo.coupahost.com/middleware/platform/api-rate-limits
updated: 2026-08-28
---
# Coupa API Rate Limits

The published rate limits on the Coupa integration API and how clients should
back off. This is the outbound Coupa API, not the AVR facade Coupa calls.

## Limits

- A default of 300 requests per minute per integration client.
- A `429` response includes a `Retry-After` header the client must honor.
