---
url: https://sf-demo.coupahost.com/middleware/avr/rest-facade
updated: 2026-08-03
---
# AVR REST Facade

The REST/JSON API that **Coupa** calls to read invoice, purchase order and
receipt data. Each call is translated by the AWS middleware into a SOAP call to
the on-prem AVR service and the response translated back to JSON. The facade
holds no data of its own.

The on-prem service behind it is described in the AVR SOAP service document;
element-level translation is in the AVR field mapping.

## Request model

This is a **pull** integration and every call is **synchronous**. **Coupa calls**
the middleware and blocks until the response returns; the middleware calls AVR
and blocks in turn. Nothing is pushed to Coupa, nothing is queued, and there is
no batch mode.

Two consequences follow, and both come up regularly:

- There is no backlog to drain when AVR is down. Requests simply fail, and
  Coupa retries them later on its own schedule. Nothing accumulates in AWS.
- Middleware latency is dominated by AVR. The middleware adds roughly 40ms of
  translation overhead; everything else in a response time is the on-prem call.

## Endpoints

All endpoints sit under `https://avr-api.sf-demo-middleware.com/v1/` and are
fronted by **API Gateway**, which terminates TLS, applies the usage plan and
forwards to the Lambda handlers.

- `GET /v1/invoices/{invoiceNumber}?supplierId=` — invoice header, lines, tax
  and payment status. Backed by `GetInvoiceDetail`.
- `GET /v1/purchase-orders/{poNumber}` — PO header, lines and distributions.
  Backed by `GetPurchaseOrder`.
- `GET /v1/receipts/{receiptNumber}` — a single receipt. Backed by `GetReceipt`.
- `GET /v1/purchase-orders/{poNumber}/receipts` — all receipts against a PO.
  Also backed by `GetReceipt`, using its PO-number form.
- `GET /v1/suppliers/{supplierId}` — supplier name, tax ID and remit-to
  addresses. Backed by `GetSupplierMaster`.

Authentication is an API key issued to Coupa through an API Gateway usage plan,
sent as `x-api-key`. The usage plan caps Coupa at 50 requests per second with a
burst of 100; exceeding it returns `429`.

## Timeouts and errors

The middleware applies a **30 seconds** upstream timeout to every AVR SOAP
call. When AVR does not respond within it, the middleware returns HTTP **504**
with the error code **`AVR-TIMEOUT`**, and Coupa retries on its own schedule.
The middleware does not queue or replay the request — there is nowhere for it
to wait.

The full set of responses Coupa can receive:

- `200` — translated AVR response
- `400` / `AVR-BAD-REQUEST` — malformed identifier, for example a non-numeric
  invoice number
- `401` / `AVR-UNAUTHORIZED` — missing or invalid `x-api-key`
- `404` / `AVR-NOT-FOUND` — AVR returned no record for that identifier. Often a
  timing issue rather than a real absence: an invoice keyed in PeopleSoft
  minutes earlier may not have propagated to AVR yet.
- `429` / `AVR-THROTTLED` — usage plan exceeded
- `502` / `AVR-SOAP-FAULT` — AVR returned a SOAP fault; the fault string is
  passed through in the response body
- `503` / `AVR-UNREACHABLE` — the on-prem service could not be reached at all,
  which normally means Direct Connect rather than AVR itself
- `504` / `AVR-TIMEOUT` — the 30 second upstream timeout elapsed

Every non-200 response carries the middleware request ID in `x-request-id`,
which is the key to finding the matching entry in CloudWatch.
