---
url: https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0010003
updated: 2026-09-03
---
# ServiceNow Integration Runbook

Operational procedures for both ServiceNow integrations — the product sync
pushing catalog content into ServiceNow, and the request sync pulling approved
requests into Coupa. Field definitions live in the integration documents; this
document covers what to do when a run fails.

## Error codes

Codes are emitted by both jobs and appear in the run summary and in CloudWatch
logs under the log group `/aws/lambda/snow-sync`.

- `SNOW-401` — authentication rejected. The ServiceNow integration user's
  credential has expired or been locked. No retry will succeed; rotate the
  credential in Secrets Manager and re-run.
- `SNOW-409` — duplicate catalog item. ServiceNow already holds an item
  correlated to the same Coupa item identifier that the product sync matches
  on. Almost always caused by an item created manually in ServiceNow that was
  later mapped, or by a replay run against a batch that already succeeded.
- `SNOW-422` — field validation failed. A mapped value is not in the target
  choice list; the unit of measure and category mappings are the usual
  culprits. The run summary names the offending field and value.
- `SNOW-424` — unmapped cost centre. The request's cost centre has no entry in
  `snow_costcentre_map.yaml`, so no Coupa account can be chosen. The request is
  skipped, not failed.
- `SNOW-429` — rate limited by ServiceNow. Handled automatically with backoff;
  only escalate if it persists beyond one hour.
- `SNOW-503` — ServiceNow unavailable. The batch is left on the queue and
  retried; no action needed unless the outage exceeds the retry window.

## Replay procedure

Use this to re-run a batch after fixing the underlying cause. It is safe to
replay a batch that partially succeeded: pushes are keyed on the correlation
identifier and update rather than insert when a match already exists.

1. Find the failed batch identifier in the run summary, or take the message
   from `snow-product-dlq` or `snow-request-dlq`.
2. Confirm the cause is fixed. For a `SNOW-409`, first open the existing
   ServiceNow catalog item and confirm it is genuinely the same product — if it
   is a different product that collided, the mapping is wrong and replaying
   will not help.
3. Clear the stale entry from the sync watermark table
   `snow-sync-watermarks` for the affected content group. Replaying without
   clearing the watermark re-runs the batch but leaves the watermark ahead of
   it, so the next scheduled run skips the same records again.
4. Invoke the replay: `aws lambda invoke --function-name snow-sync-replay
   --payload '{"batch":"<batch-id>"}'`.
5. Confirm the run summary reports zero failures, then delete the DLQ message.

Replaying the request sync creates no duplicate requisitions: the Coupa
requisition carries the ServiceNow request identifier, and an existing match
causes an update instead of a create.

## When to escalate

Page the Middleware Platform on-call through PagerDuty when any of the
following hold:

- `snow-product-dlq` or `snow-request-dlq` depth exceeds 50 messages
- `SNOW-401` recurs after a credential rotation, which usually means the
  integration user has been disabled rather than expired
- `snow-request-pull` has not completed successfully for two hours, since
  approved requests are accumulating with no visible sign to the requester
- any `SNOW-409` where the colliding items are genuinely different products —
  this indicates a mapping fault that will keep recurring and needs the catalog
  team, not a replay
