# AVR SOAP Service

**AVR** is an on-premises application that exposes accounts-payable data —
invoices, purchase orders and receipts — over a SOAP web service. The AWS
middleware is its only consumer: it calls AVR and translates the response into
the REST/JSON that Coupa reads. Coupa never talks to AVR directly and has no
knowledge of SOAP.

This document covers the on-prem side. The REST API Coupa calls is described in
the AVR REST facade document, and the element-by-element translation in the AVR
field mapping.

## Where it runs

- The service runs in the on-prem data centre, fronted by the internal load
  balancer `avr-ws.internal`
- The middleware reaches it over AWS Direct Connect, through a private
  subnet route; there is no public path to AVR and no VPN fallback
- The WSDL is published at `http://avr-ws.internal/AvrFinancialService?wsdl`
  and is versioned; the middleware pins version `v4`
- Authentication is WS-Security username token; the credential lives in
  Secrets Manager as `avr/ws-credentials`

Because the only route is Direct Connect, a circuit failure takes the whole
integration down rather than degrading it. There is no cached copy of AVR data
in AWS — every Coupa request results in a live call to the on-prem service.

## Operations

The middleware uses four of the operations the WSDL exposes:

- `GetInvoiceDetail` — takes `InvoiceNbr` and `SupplierId`, returns the invoice
  header, all lines, tax detail and payment status. The heaviest operation; a
  large invoice can return several hundred lines.
- `GetPurchaseOrder` — takes `PoNbr`, returns the PO header, lines and
  distribution rows, including the accounting string for each distribution.
- `GetReceipt` — takes `ReceiptNbr`, or `PoNbr` for all receipts against a
  purchase order. Returns received quantities and dates.
- `GetSupplierMaster` — takes `SupplierId`, returns supplier name, tax ID and
  remit-to addresses. Used to enrich invoice responses where Coupa needs the
  supplier's tax identity.

Operations the WSDL exposes but the middleware deliberately does not call:
`PostInvoice`, `UpdatePaymentStatus` and `VoidInvoice`. AVR is a read-only
source in this integration — nothing flows from Coupa back into AVR through
this path.

## Source systems

AVR is a facade over several on-prem systems rather than a database in its own
right:

- **PeopleSoft Financials** supplies invoice headers, lines, tax detail and all
  purchase order data. It is the system of record for both.
- The **expense reporting system** supplies employee expense reports, which AVR
  presents as invoices with a synthetic supplier identifier so that Coupa can
  treat them uniformly.
- The **on-prem receiving application** supplies receipt data, which AVR joins
  to purchase orders before returning it.
- The **supplier master** supplies tax identity and remit-to addresses for
  `GetSupplierMaster`.

Because AVR joins across these systems at request time, response latency varies
with the source rather than with AVR itself. Expense-report-backed invoices are
consistently the slowest, since the expense system holds attachments the
receiving and PeopleSoft paths do not.
