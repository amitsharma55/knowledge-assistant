---
url: https://sf-demo.coupahost.com/middleware/webhook-subscriptions
updated: 2026-08-01
---
# Webhook Subscriptions

Coupa can push document events to registered webhook endpoints so integrations
react without polling.

## Behavior

- A subscription registers an event type and an HTTPS endpoint.
- Delivery retries with exponential backoff for up to 24 hours.

## Failure modes

- `WEBHOOK-ENDPOINT-5XX` — the endpoint returned server errors past the retry
  window; the subscription is paused and the owner notified.
