---
url: https://sf-demo.coupahost.com/middleware/avr/field-mapping
updated: 2026-07-14
---
# AVR Field Mapping

Element-by-element translation between the AVR SOAP response and the JSON the
REST facade returns to Coupa. Applied on every call; see the AVR REST facade
for the endpoints and the AVR SOAP service for the operations behind them.

SOAP element names use AVR's own casing, which is inconsistent — some elements
abbreviate, some do not. The JSON names are normalised to camelCase regardless.

## Invoice fields

From `GetInvoiceDetail`:

- `InvoiceNbr` — the supplier invoice number; becomes `invoiceNumber`, which is
  what Coupa matches against its own invoice records
- `InvoiceHdrId` — AVR's internal header key; becomes `avrInvoiceId`
- `SupplierId` — becomes `supplierId`
- `SupplierNm` — becomes `supplierName`
- `InvcDt` — invoice date, AVR returns `MM/DD/YYYY`; becomes `invoiceDate` as
  ISO 8601
- `DueDt` — becomes `dueDate`, also converted to ISO 8601
- `InvcAmt` — becomes `totalAmount`, as a decimal string to avoid float
  rounding
- `CurrCd` — becomes `currencyCode`
- `PymtStatus` — AVR returns `P`, `U` or `H`; becomes `paymentStatus` as
  `paid`, `unpaid` or `held`
- `PoNbr` — becomes `purchaseOrderNumber` where the invoice is PO-backed; absent
  for expense-report-backed invoices
- `InvcLines/InvcLine` — becomes the `lines` array

Per invoice line:

- `LineNbr` — becomes `lineNumber`
- `ItemDesc` — becomes `description`
- `Qty` — becomes `quantity`
- `UnitPrc` — becomes `unitPrice`
- `ExtAmt` — becomes `extendedAmount`
- `TaxCd` — becomes `taxCode`
- `AcctString` — becomes `accountingString`, passed through unparsed

## Purchase order fields

From `GetPurchaseOrder`:

- `PoNbr` — becomes `purchaseOrderNumber`
- `PoHdrId` — becomes `avrPurchaseOrderId`
- `BuyerId` — becomes `buyerId`
- `PoStatus` — AVR returns `O`, `C` or `X`; becomes `status` as `open`,
  `closed` or `cancelled`
- `PoDt` — becomes `orderDate` as ISO 8601
- `PoTotal` — becomes `totalAmount`
- `PoLines/PoLine` — becomes the `lines` array, with `LineNbr`, `ItemDesc`,
  `Qty`, `UnitPrc` and `AcctString` mapped as for invoice lines
- `Distributions/Distribution` — becomes `distributions`, each with `DistPct`
  as `percentage` and `AcctString` as `accountingString`

## Receipt fields

From `GetReceipt`:

- `ReceiptNbr` — becomes `receiptNumber`
- `PoNbr` — becomes `purchaseOrderNumber`
- `RcvdDt` — becomes `receivedDate` as ISO 8601
- `RcvdQty` — becomes `receivedQuantity`
- `RcvrId` — becomes `receivedBy`, resolved to an email where the receiver is a
  known employee and left as the raw identifier where not

## Translation rules that apply everywhere

- Empty SOAP elements become absent JSON keys, never `null`. Coupa treats a
  present-but-null field as an instruction to clear a value.
- All dates convert from AVR's `MM/DD/YYYY` to ISO 8601. A date AVR returns as
  `00/00/0000`, which it does for unset dates, becomes an absent key.
- All amounts are strings in JSON, not numbers, so that a two-decimal value
  cannot be reformatted by a JSON parser.
- AVR's single-character status codes are always expanded to words. Coupa never
  sees `P`, `U`, `H`, `O`, `C` or `X`.
