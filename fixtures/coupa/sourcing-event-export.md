---
url: https://sf-demo.coupahost.com/middleware/sourcing/event-export
updated: 2026-07-12
---
# Sourcing Event Export

Exports awarded sourcing events from Coupa Sourcing to the downstream analytics
warehouse. Read-only and nightly; it writes nothing back to Coupa.

## Fields

- `sourcing_event_id`, `award_amount`, `supplier_count`, `savings_percent`,
  `commodity`, `award_date`.

## Boundaries

The export carries no contract terms; awarded events that become contracts are
handled by the Icertis contract integration, not here.
