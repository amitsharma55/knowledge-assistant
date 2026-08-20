# Icertis-Coupa Integration

Synchronizes executed contracts from **Icertis CLM** (Contract Lifecycle
Management) into **Coupa** so procurement teams can reference contract terms
when creating purchase orders and invoices. Runs against the Coupa production
tenant `acme.coupahost.com` and Icertis tenant `acme.icertis.com`.

## Sync scope

Only contracts matching **all** of the following are synced:

- `contractStatus` = `Executed` (fully signed)
- `contractType` in (`Master Service Agreement`, `Statement of Work`, `Purchase Agreement`)
- `region` in (`NA`, `EMEA`, `APAC`)
- `effectiveDate` <= today AND (`expirationDate` is null OR `expirationDate` >= today)

Contracts in `Draft`, `In Review`, `Expired`, or `Terminated` states are
skipped. Amendments propagate as new versions on the same Coupa contract.

## Fields sent to Coupa

Every push maps the following Icertis fields to Coupa contract fields:

- `icertis_contract_id` — Icertis GUID; stored on Coupa as `external-id`
- `contract_number` — human-readable Icertis number, e.g. `CT-2026-00483`
- `supplier_name` — resolved to Coupa supplier via tax ID match
- `supplier_tax_id` — used for the supplier lookup on Coupa side
- `buyer_entity` — one of our legal entities; mapped to a Coupa content group
- `effective_date` / `expiration_date` — ISO 8601 dates
- `total_contract_value` — decimal, in contract currency
- `currency_code` — ISO 4217, e.g. `USD`, `EUR`, `INR`
- `payment_terms` — mapped from Icertis clause library to Coupa payment terms
- `governing_law` — jurisdiction string
- `contract_pdf_url` — signed URL to the executed PDF; Coupa fetches and attaches
- `owner_email` — Icertis contract owner; matched to a Coupa user

## Jobs

| Job                        | Schedule (UTC) | Description                                               |
|----------------------------|----------------|-----------------------------------------------------------|
| `icertis-coupa-incremental` | every 15 min   | Pushes contracts changed since last watermark              |
| `icertis-coupa-daily-full`  | daily 01:30    | Full sweep of all in-scope contracts; catches missed events |
| `icertis-coupa-status-sync` | every 30 min   | Pulls Coupa contract-attach events back to Icertis         |
| `icertis-coupa-amendment`   | every 15 min   | Handles amendment versioning; runs after incremental       |

## Today's typical volume

- ~120–180 contracts pushed by `icertis-coupa-incremental` over the day
- Full sweep processes ~14,000 in-scope contracts nightly, of which ~200
  are updated back into Coupa
- Failed pushes are retried up to 5 times with exponential backoff; anything
  still failing lands in the `icertis-coupa-dlq` SQS queue
