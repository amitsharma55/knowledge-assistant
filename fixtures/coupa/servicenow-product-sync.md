---
url: https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0010001
updated: 2026-07-18
---
# ServiceNow Product Sync

Pushes purchasable product and catalog items from **Coupa** into the
**ServiceNow** Service Catalog, so that requesters browsing ServiceNow see the
same items, prices and suppliers that procurement maintains in Coupa. Runs
against the Coupa tenant `sf-demo.coupahost.com` and the ServiceNow instance
`https://sf-demo.service-now.com`.

This is the Coupa-to-ServiceNow direction only. Request data flowing the other
way is handled by the ServiceNow Request Sync.

## What syncs

An item is pushed when **all** of the following hold:

- `item_status` = `active` in Coupa
- the item belongs to a content group mapped in `snow_catalog_map.yaml`
- the item has a supplier with a resolved ServiceNow `sys_id`
- `list_price` is present and greater than zero

Items that are inactive, unpriced, or belong to an unmapped content group are
skipped and counted in the run summary. Deactivating an item in Coupa pushes an
update setting `active` to `false` in ServiceNow; it does not delete the
catalog item, because ServiceNow request history references it.

## Fields sent to ServiceNow

Every catalog item push carries the following fields:

- `sys_id` — the ServiceNow record identifier; populated by ServiceNow on
  first insert and stored back on the Coupa item for subsequent updates
- `u_coupa_item_id` — the Coupa item UUID; the correlation key for every
  update and the field the duplicate check runs against
- `u_coupa_supplier_id` — Coupa supplier UUID
- `name` — item name as maintained in Coupa
- `short_description` — first 160 characters of the Coupa item description
- `description` — the full item description, plain text
- `catalog` — target ServiceNow catalog, from `snow_catalog_map.yaml`
- `category` — ServiceNow category sys_id, mapped from the Coupa commodity code
- `list_price` — decimal, in the item's currency
- `currency` — ISO 4217, for example `USD`
- `unit_of_measure` — mapped from the Coupa UOM list to the ServiceNow choice
  list
- `manufacturer_part_number` — supplier part number where present
- `vendor` — ServiceNow vendor sys_id, resolved from `u_coupa_supplier_id`
- `active` — boolean; mirrors `item_status`

## Jobs

- `snow-product-push` — every 30 minutes. Pushes items created or changed since
  the last watermark.
- `snow-product-full` — daily at 03:45 UTC. Full sweep of all in-scope items;
  catches anything missed when a webhook was dropped.
- `snow-category-refresh` — daily at 03:15 UTC. Refreshes the commodity code to
  ServiceNow category mapping before the full sweep runs.

`snow-product-push` holds a watermark per content group, so a failure in one
group does not stall the others.

## Volume

- 4,000–4,500 active catalog items in scope on a normal day
- 60–120 item changes pushed per day by `snow-product-push`
- The nightly full sweep compares all in-scope items and typically finds fewer
  than 10 drift corrections
- A push that fails is retried three times with exponential backoff before it
  is written to the `snow-product-dlq` SQS queue
