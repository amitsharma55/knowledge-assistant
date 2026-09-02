# Icertis Resync Runbook

What to do when the Icertis contract integration fails. Most incidents on this
integration are resync failures rather than load failures, because the load
retries cleanly while the confirmation back to Icertis has a narrower window.

Phase definitions are in the Icertis contract overview; field mappings are in
the Icertis field mapping document.

## Error codes

- `ICT-401` — Icertis authentication rejected. The service principal's secret
  has expired. Rotate it in Secrets Manager; no retry succeeds before that.
- `ICT-404` — the contract disappeared between fetch and load, normally because
  it was terminated in Icertis mid-run. Safe to discard; it will not be
  re-fetched.
- `ICT-409` — the Coupa contract already exists with that external ID. Expected
  when a load is replayed; the load converts to an update and no action is
  needed. Investigate only if the existing Coupa contract has a different
  contract number.
- `ICT-422` — supplier could not be resolved from `supplier_tax_id`. The
  contract stays staged until the supplier exists in Coupa. This is the most
  common load failure and needs the supplier master team, not the middleware
  team.
- `ICT-503` — **resync confirmation failed**. The contract loaded into Coupa
  successfully but the confirmation call back to Icertis did not land, so
  Icertis still shows it as pending load. The contract is **orphaned**: present
  and usable in Coupa, invisible as loaded to anyone looking in Icertis. No
  data is lost, and the contract must not be re-loaded.

## Recovering an orphaned contract

`ICT-503` is handled automatically by `icertis-resync-retry`, which runs hourly
at :40 and re-sends the confirmation for every contract that loaded but never
confirmed. In almost all cases the next run clears it with no intervention.

Intervene only when a contract has failed resync for more than four hours:

1. Confirm the contract really is in Coupa. Search Coupa by the Icertis GUID as
   the external ID. If it is absent, this is a load failure wearing the wrong
   code — treat it as `ICT-422` or `ICT-409` instead.
2. Check whether Icertis is accepting writes at all. A broad Icertis outage
   shows as every resync failing, not one.
3. Force a single retry:
   `aws lambda invoke --function-name icertis-resync-retry
   --payload '{"contractId":"<icertis-guid>"}'`.
4. If that fails with `ICT-401`, rotate the credential and repeat.

Never resolve an orphaned contract by re-running the load. The load is what
creates the Coupa contract; running it again against a contract that already
loaded produces `ICT-409` at best, and before the phases were separated in
2026-02 it produced duplicate contracts in Coupa.

## When to escalate

Page the Middleware Platform on-call through PagerDuty when:

- more than 20 contracts are in resync-failed state, which indicates an Icertis
  API problem rather than individual failures
- `icertis-load-dlq` depth exceeds 25 messages
- the nightly full sweep has not completed by 04:00 UTC
- any contract has been orphaned for more than 24 hours, since procurement may
  be raising orders against a contract that Icertis believes was never loaded
