---
url: https://sf-demo.coupahost.com/middleware/ocr/invoice-intake
updated: 2026-07-28
---
# Invoice OCR Intake

Captures supplier PDF invoices, extracts fields by OCR, and creates draft
invoices in Coupa for review. It is an inbound document pipeline, unrelated to
the AVR read path that serves invoice data to Coupa on demand.

## Flow

- A PDF arriving in the intake mailbox is OCR'd into a draft with
  `supplier_id`, `invoice_number`, `invoice_date`, and line amounts.
- Low-confidence fields are marked for human verification before the draft is
  submitted.

## Failure modes

- `OCR-LOW-CONFIDENCE` — the extraction confidence fell below threshold; the
  draft is routed to the AP review queue rather than auto-submitted.
