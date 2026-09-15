---
url: https://sf-demo.coupahost.com/middleware/pir/overview
updated: 2026-08-12
---
# PIR Payment Router Overview

The Payment Integration Router (PIR) is the Coupa AWS Middleware component that
forwards approved payment instruction files from Coupa to the receiving bank
profiles. Unlike the synchronous AVR read path, PIR is asynchronous and holds a
queue, so a fault leaves a backlog to drain once the cause clears.

## Flow

- Coupa emits an approved payment batch to the `pir-inbound` SQS queue.
- PIR validates each instruction against the receiving bank profile schema.
- Valid instructions are forwarded to the bank endpoint; the acknowledgement is
  written back to Coupa as a payment status update.

## Fields forwarded

- `payment_batch_id`, `remittance_ref`, `beneficiary_id`, `settlement_date`,
  `currency`, `amount`, `bank_profile_key`.

PIR does not transform amounts or currencies; it validates and forwards only.
Contract and product data never move through PIR — those are Icertis and the
ServiceNow product sync respectively.
