---
url: https://sf-demo.coupahost.com/middleware/pir/runbook
updated: 2026-08-19
---
# PIR Payment Router Runbook

What to do when Coupa reports failures submitting payment instruction files
through the Payment Integration Router.

## PIR-SCHEMA-REJECT

The payment file failed validation at the router (`422`). Common causes: a
missing `remittance_ref`, a currency the receiving bank profile does not carry,
or a `settlement_date` on a bank holiday. Fix the source record in Coupa and
resubmit; the router validates and forwards, it does not transform.

## PIR-BANK-UNREACHABLE

The bank endpoint did not answer (`503`) and nothing was enqueued downstream.
Check the bank profile's endpoint health before replaying the batch.

## When to escalate

Page the Payments Platform on-call when `PIR-BANK-UNREACHABLE` persists past one
drain cycle, or when the `422` rate exceeds 10% over 15 minutes.
