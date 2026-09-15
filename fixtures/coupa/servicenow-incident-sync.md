---
url: https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0010050
updated: 2026-08-16
---
# ServiceNow Incident Sync

A separate, one-directional feed that mirrors Coupa middleware incidents into
ServiceNow for reporting. It is unrelated to the product and request syncs,
which move catalog and purchased-product data.

## What it sends

- On an incident state change, the sync posts `sys_id`, `short_description`,
  `severity`, `opened_at`, and `assignment_group` to the ServiceNow incident
  table.

## What it does not do

This feed carries no catalog data and no purchased-product data. It never
writes `list_price` or supplier fields, and it holds no watermark table of its
own.
