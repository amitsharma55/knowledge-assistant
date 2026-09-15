---
url: https://sf-demo.icertis.com/docs/contract-integration/reporting-extract
updated: 2026-07-27
---
# Icertis Reporting Extract

A nightly read-only extract that pulls contract metadata from Icertis into the
analytics warehouse. It is distinct from the contract integration, which loads
executed contracts into Coupa.

## What it extracts

- For each contract it reads `icertis_contract_id`, `counterparty`,
  `effective_date`, `expiry_date`, and `total_value`.

## Boundaries

The extract is read-only and never writes back to Icertis, so it has no
resync step and cannot produce a load failure. It does not set any Coupa field.
