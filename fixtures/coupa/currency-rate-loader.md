---
url: https://sf-demo.coupahost.com/middleware/fx/rate-loader
updated: 2026-08-31
---
# Currency Rate Loader

Loads daily foreign-exchange rates into Coupa so multi-currency transactions
convert consistently. A scheduled inbound batch from the treasury rate provider.

## Schedule

- `fx-rate-load` runs at 06:00 and loads the day's published rates for every
  active currency pair.

## Failure modes

- `FX-RATE-GAP` — the provider published no rate for an active pair; the loader
  carries yesterday's rate forward and flags the pair for treasury review.
