---
url: https://sf-demo.coupahost.com/middleware/avr/runbook
updated: 2026-09-01
---
# AVR Runbook

What to do when Coupa reports failures reading invoice, purchase order or
receipt data. Because the AVR integration is synchronous and holds no data,
almost every incident here is either the on-prem service or the network path to
it — there is no queue to inspect and no backlog to drain.

Endpoints and response codes are in the AVR REST facade; the on-prem service is
described in the AVR SOAP service document.

## Diagnosing a failure

Start by separating the three things that look identical from Coupa's side:

1. **AVR is slow** — requests return `504` with `AVR-TIMEOUT` after 30 seconds.
   Some requests still succeed. Latency in CloudWatch shows a rising p99 before
   the timeouts start.
2. **AVR is down** — requests return `502` with `AVR-SOAP-FAULT`, and the fault
   string names the failing source system. AVR itself is answering.
3. **The network path is down** — requests return `503` with `AVR-UNREACHABLE`
   and no AVR log entry exists for them at all, because nothing arrived. This
   means Direct Connect, not AVR.

The `x-request-id` on any failed response is the key for CloudWatch; search the
log group `/aws/lambda/avr-facade` for it to get the full SOAP request and
fault.

## AVR-TIMEOUT

The 30 second upstream timeout elapsed. Handle by cause:

- **A single slow operation.** `GetInvoiceDetail` on a very large invoice is the
  usual offender, and expense-report-backed invoices are the slowest of those
  because the expense system holds attachments the other sources do not. Confirm
  by finding the invoice number in the CloudWatch entry and checking its line
  count. There is no fix beyond re-requesting; the timeout is not configurable
  per request.
- **Broad slowness.** If p99 latency has been climbing across all operations,
  the source system behind AVR is degraded rather than AVR. PeopleSoft batch
  windows are the common cause, and they resolve on their own.
- **Coupa retrying too fast.** A `504` followed immediately by more `504`s from
  the same identifier is Coupa retrying into an already-loaded service. Nothing
  to do in the middleware.

## AVR-SOAP-FAULT

AVR answered with a fault. The fault string is passed through in the response
body and names the source. Common ones:

- `PSFT_UNAVAILABLE` — PeopleSoft is not answering AVR. Invoice and PO reads
  fail; receipt reads may still succeed since they come from a different source.
- `EXPENSE_TIMEOUT` — the expense system did not answer within AVR's own
  internal timeout. Affects expense-report-backed invoices only.
- `INVALID_CREDENTIAL` — the WS-Security token was rejected. Rotate
  `avr/ws-credentials` in Secrets Manager.

A fault naming one source while others succeed is not a middleware incident.
Raise it with the owning application team.

## AVR-UNREACHABLE

The middleware could not reach `avr-ws.internal` at all. There is no VPN
fallback, so this is Direct Connect until proven otherwise:

1. Check the Direct Connect virtual interface state in the AWS console.
2. Check whether other on-prem-dependent integrations are also failing. If they
   are, it is the circuit.
3. If the circuit is healthy, check the internal load balancer — AVR may be up
   with no healthy targets behind it.

This is the only AVR failure mode that warrants paging immediately, because
nothing recovers on its own and every Coupa read fails while it lasts.

## When to escalate

Page the Middleware Platform on-call through PagerDuty when:

- `AVR-UNREACHABLE` appears at all
- the `504` rate exceeds 5% of requests over 15 minutes
- `INVALID_CREDENTIAL` recurs after a rotation
- Coupa reports missing invoices that return `404` but are visible in
  PeopleSoft, which suggests AVR's join is stale rather than the data absent
