---
url: https://sf-demo.icertis.com/docs/contract-integration/renewal-tracker
updated: 2026-08-18
---
# Contract Renewal Tracker

A scheduled job that flags Icertis contracts approaching expiry so owners can
start a renewal. It reads expiry dates only and never loads contracts into
Coupa, so it has no resync step.

## Behavior

- `renewal-scan` runs weekly and lists contracts expiring within 90 days.
- Owners receive a task in Icertis; no Coupa record is created by this job.

## Boundaries

This tracker does not read `icertis_contract_id` for loading; it uses it only
to link the reminder back to the source contract.
