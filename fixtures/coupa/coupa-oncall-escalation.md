# Coupa Middleware On-Call and Escalation

Who owns the Coupa AWS Middleware integrations, how to reach them, and what is
worth waking someone for. Integration-specific failure procedures live in the
ServiceNow, Icertis and AVR runbooks; this document covers who to involve.

## Ownership and on-call

The **Middleware Platform** team owns all three integrations and holds the
pager for them.

- Paging goes through **PagerDuty**, service `coupa-aws-middleware`
- The rotation is weekly, handing over Wednesdays at 10:00 CT
- Business-hours questions go to the `#coupa-middleware` channel rather than
  the pager
- The escalation policy pages the primary, then the secondary after 15 minutes,
  then the team lead after a further 15

The Middleware Platform team owns the middleware only. It does not own Coupa
itself, ServiceNow, Icertis, or the on-prem systems behind AVR — those have
their own owners, listed below, and most incidents that look like middleware
failures turn out to belong to one of them.

## Escalation by integration

**ServiceNow** — page for `snow-product-dlq` or `snow-request-dlq` depth above
50, for `SNOW-401` recurring after a credential rotation, or for
`snow-request-pull` stalled beyond two hours. A `SNOW-409` where the colliding
items are genuinely different products is a catalog data problem: raise it with
the catalog team, not the pager.

**Icertis** — page for more than 20 contracts in resync-failed state, for
`icertis-load-dlq` depth above 25, or for any contract orphaned longer than 24
hours. `ICT-422` supplier resolution failures are supplier master data and
belong to the supplier master team; they will not resolve by retrying.

**AVR** — page immediately for `AVR-UNREACHABLE`, since nothing recovers on its
own while the path to on-prem is down. Page for a `504` rate above 5% over 15
minutes. A SOAP fault naming one source system while others succeed belongs to
that system's team.

## Who owns what else

- **Coupa application** — the Procurement Systems team. Anything about Coupa
  configuration, approval chains or supplier records.
- **ServiceNow platform** — the ITSM Platform team. Catalog structure, choice
  lists and the integration user's account.
- **Icertis** — the Contract Systems team. Contract templates, clause library
  and the Icertis API credential.
- **PeopleSoft and the expense system** — the Financial Systems team. Every AVR
  SOAP fault naming `PSFT_UNAVAILABLE` or `EXPENSE_TIMEOUT` is theirs.
- **Direct Connect and network** — the Network Engineering team, paged
  separately. `AVR-UNREACHABLE` usually means paging them too.

## What not to page for

- Individual record failures. A single contract failing `ICT-422`, or one
  request skipped for an unmapped cost centre, is reported daily and handled in
  business hours.
- `SNOW-429` or `AVR-THROTTLED` on their own. Both are handled by backoff and
  resolve without intervention unless they persist beyond an hour.
- The nightly full sweeps running long. They are expected to overlap the
  maintenance window occasionally; only page if one has not completed by 04:00
  UTC.
- Coupa users reporting that an item is missing from the ServiceNow catalog
  within 30 minutes of it being created. The push runs every 30 minutes, so
  this is usually just timing.
