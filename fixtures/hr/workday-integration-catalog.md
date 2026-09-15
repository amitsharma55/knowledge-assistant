---
url: https://wd5-impl.workday.com/sf_demo/d/integration-catalog
updated: 2026-08-28
---
# Integration Catalog

An index of the outbound Workday integrations other than payroll. Each entry
names its downstream system and schedule; the payroll integration is documented
separately.

## Entries

- `workday-benefits-carrier-feed` — nightly, to each benefits carrier.
- `workday-badge-provisioning` — every 15 minutes, to the badge system for new
  and terminated workers.
- `workday-directory-export` — twice daily, to the corporate directory.

A failed integration here retries on its own schedule and never holds up an
unrelated feed.
