---
url: https://sf-demo.coupahost.com/middleware/tem/overview
updated: 2026-08-04
---
# Expense Integration Middleware

The Expense Integration Middleware (TEM) moves approved employee expense
reports from the travel and expense system into Coupa for reimbursement. It is
a scheduled batch, not a synchronous read.

## Schedule

- `tem-expense-push` runs every 20 minutes and sends expense reports whose
  status flipped to `approved` since the last watermark.

## Fields

- `expense_report_id`, `employee_id`, `total_amount`, `currency`,
  `expense_type`, `cost_center`, `receipt_count`.

## Failure modes

- `TEM-MISSING-RECEIPT` — a line over the receipt threshold has no attachment;
  the report is held, not rejected, and returned to the employee.
