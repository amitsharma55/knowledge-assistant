---
url: https://sf-demo.icertis.com/docs/contract-integration/field-mapping
updated: 2026-08-11
---
# Icertis Field Mapping

Field-level mapping between an Icertis contract record and the Coupa contract
it becomes. Applied during the load phase of the Icertis contract integration;
see the Icertis contract overview for how the phases fit together.

## Fields sent to Coupa

Every load maps the following Icertis fields onto the Coupa contract:

- `icertis_contract_id` — the Icertis GUID; stored on the Coupa contract as
  `external-id`, and the correlation key for every later update and for the
  resync confirmation
- `contract_number` — human-readable Icertis number, for example `CT-2026-00483`
- `contract_name` — becomes the Coupa contract name
- `contract_type` — mapped to a Coupa contract type through
  `icertis_type_map.yaml`
- `supplier_name` — informational; the supplier link is resolved by tax ID
- `supplier_tax_id` — used to resolve the Coupa supplier; a contract whose tax
  ID matches no Coupa supplier fails the load rather than creating one
- `buyer_entity` — the contracting legal entity; mapped to a Coupa content group
- `effective_date` — ISO 8601 date
- `expiration_date` — ISO 8601 date, or null for evergreen contracts
- `total_contract_value` — decimal, in the contract currency
- `currency_code` — ISO 4217, for example `USD`, `EUR`, `INR`
- `payment_terms` — mapped from the Icertis clause library to a Coupa payment
  terms code
- `governing_law` — jurisdiction string, stored as a custom field
- `contract_pdf_url` — time-limited signed URL to the executed PDF; Coupa
  fetches and attaches the document during the load
- `owner_email` — the Icertis contract owner, matched to a Coupa user by email
- `parent_contract_id` — populated on amendments; carries the
  `icertis_contract_id` of the contract being amended, and is what makes the
  amendment a new version rather than a new contract

## Fields returned by Coupa

The load response is held against the staged record and used by the resync
phase:

- `coupa_contract_id` — Coupa's own identifier; sent back to Icertis on
  confirmation
- `coupa_contract_version` — increments when an amendment lands on an existing
  contract
- `coupa_supplier_id` — the supplier resolved from `supplier_tax_id`

## Fields written back to Icertis

The resync phase writes exactly three fields and nothing else:

- `integrationStatus` — set to `LoadedToCoupa`
- `externalSystemId` — set to `coupa_contract_id`
- `lastSyncedUtc` — timestamp of the confirmation

## Values that are deliberately not mapped

- Coupa never receives Icertis clause text. Only the mapped `payment_terms`
  code and `governing_law` string cross the boundary; the executed PDF carries
  everything else.
- Icertis approval history is not mapped. Coupa records who loaded the
  contract, not who approved it in Icertis.
- Contract amounts are never converted between currencies. `currency_code`
  travels with `total_contract_value` and conversion is left to Coupa reporting.
