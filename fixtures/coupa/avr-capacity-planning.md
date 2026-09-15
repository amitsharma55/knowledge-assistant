---
url: https://sf-demo.coupahost.com/middleware/avr/capacity-planning
updated: 2026-08-24
---
# AVR Capacity Planning

How the AVR facade is sized, and the signals used to decide when to raise its
Lambda concurrency. This is a planning document, not a runbook: for what to do
during an incident, see the AVR runbook.

## Signals we watch

- Sustained `GetInvoiceDetail` volume above 40 requests/second over a business
  hour is the trigger to review reserved concurrency.
- A slow climb in p95 latency (not the sharp spike that precedes an
  `AVR-TIMEOUT`) usually means the on-prem source is warming, not that AVR is
  undersized.

## What capacity does not fix

Raising concurrency does nothing for a source-system slowdown; the request
still waits on PeopleSoft. Capacity planning is about the facade's own headroom,
not the systems behind it.
