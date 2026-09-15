---
url: https://sf-demo.coupahost.com/middleware/supplier/onboarding-sync
updated: 2026-08-05
---
# Supplier Onboarding Sync

The Supplier Onboarding Sync moves newly approved suppliers from Coupa into the
downstream vendor master. It is a batch integration that runs on a schedule,
not a synchronous read like AVR.

## Schedule

- `supplier-onboard-push` runs every 30 minutes and sends suppliers whose
  onboarding status flipped to `approved` since the last watermark.

## Fields

- `coupa_supplier_id`, `legal_name`, `tax_id`, `remit_to_address`,
  `payment_terms`, `default_currency`, `w9_on_file`.

## Failure modes

- `SUP-DUP-TAXID` — the vendor master already holds a supplier with that
  `tax_id`. Confirm it is the same legal entity before clearing the watermark
  and replaying.
