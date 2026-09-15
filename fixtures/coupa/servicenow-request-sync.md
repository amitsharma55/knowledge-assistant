---
url: https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0010002
updated: 2026-08-08
---
# ServiceNow Request Sync

Pulls approved **ServiceNow** request items for purchased product into
**Coupa**, so that a request raised and approved in ServiceNow becomes a
requisition in Coupa without anyone rekeying it. This is the
ServiceNow-to-Coupa direction; catalog content flowing the other way is handled
by the ServiceNow Product Sync.

Runs against `https://sf-demo.service-now.com` and the Coupa tenant
`sf-demo.coupahost.com`.

## What syncs

A ServiceNow request item is pulled when **all** of the following hold:

- `state` = `approved` — requests still in `requested` or `in_process` are
  ignored until approval completes
- the requested catalog item carries a `u_coupa_item_id`, meaning it originated
  from the product sync
- `requested_for` resolves to an active Coupa user by email
- the request has a cost centre that maps to a Coupa account via
  `snow_costcentre_map.yaml`

Requests for items that never came from Coupa are skipped: there is no Coupa
item to requisition against. These are reported daily to the catalog team
rather than failed, because they usually mean a catalog item was created
directly in ServiceNow by mistake.

## Fields sent to Coupa

Each pulled request creates a Coupa requisition line carrying:

- `u_snow_request_id` — the ServiceNow `sys_id` of the request item; stored on
  the Coupa requisition as the external reference
- `u_snow_request_number` — human-readable ServiceNow number, for example
  `RITM0042317`
- `requested_for_email` — resolved to the Coupa requester
- `requested_by_email` — the person who raised the request, where different
- `coupa_item_id` — from the catalog item's `u_coupa_item_id`
- `quantity` — integer, from the request item
- `unit_price` — decimal; taken from Coupa's current item price, not from the
  ServiceNow record, so an out-of-date catalog price cannot flow back in
- `currency` — ISO 4217
- `need_by_date` — ISO 8601 date, from the ServiceNow due date
- `cost_centre` — mapped to a Coupa account
- `delivery_location` — ServiceNow location mapped to a Coupa address code
- `justification` — free text from the request

## Jobs

- `snow-request-pull` — every 10 minutes. Pulls newly approved request items and
  creates the matching Coupa requisitions.
- `snow-request-status-push` — every 15 minutes. Pushes Coupa requisition and
  purchase order status back onto the originating ServiceNow request, so the
  requester sees progress without leaving ServiceNow.
- `snow-request-reconcile` — daily at 04:10 UTC. Compares approved requests from
  the last seven days against created requisitions and reports any that never
  landed.

## Volume

- 300–500 approved request items pulled per day by `snow-request-pull`
- 1,200–1,600 status updates pushed per day, since a single requisition
  generates several status transitions
- Roughly 15 requests per day are skipped for an unmapped cost centre, which is
  the most common reason a requester reports that "nothing happened"
- Failed pulls are retried three times, then land in `snow-request-dlq`
