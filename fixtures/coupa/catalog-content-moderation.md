---
url: https://sf-demo.coupahost.com/middleware/catalog/content-moderation
updated: 2026-07-22
---
# Catalog Content Moderation

Screens hosted-catalog item submissions for policy violations before they are
published to buyers. Operates on catalog items but is not the ServiceNow
product sync, which moves items to ServiceNow.

## Checks

- Blocked-commodity screening against the restricted-category list.
- Image and description profanity screening.

## Failure modes

- `MOD-BLOCKED-COMMODITY` — the item falls in a restricted category; it is
  rejected with the offending category returned to the supplier.
