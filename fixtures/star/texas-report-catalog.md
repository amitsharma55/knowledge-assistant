---
url: https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0020007
updated: 2026-07-16
---
# Texas Report Catalog

The reports the Texas data call produces, in the order they run. Each is a
single SQL file executed against the staged parquet, writing one output dataset
that is published to QuickSight for review before submission to TDI.

Report naming is prefixed `tx-` throughout, so Texas and other states' outputs
cannot be confused in S3 or in QuickSight.

## Reports

The Texas data call produces seven reports, all prefixed `tx-`:

- **`tx-premium-summary`** — written and earned premium by line of business,
  coverage code and county. The headline report and the one TDI reviews first.
  Split into ceded and retained sections, which are never combined into a
  single total.
- **`tx-catastrophe-exposure`** — exposure and named storm deductibles by
  county and tier, with tier-1 coastal counties aggregated separately. Added in
  2026-06 and now the report that takes longest to reconcile.
- `tx-loss-detail` — paid and incurred losses by county and coverage, with
  named storm losses separated from other perils. Carries five years of loss
  history, which is what the mainframe extract exists to supply.
- `tx-policy-count` — in-force policy counts by county and line, with ceded and
  retained shown separately.
- `tx-coverage-breakdown` — exposure by coverage code and limit band, used by
  TDI to assess concentration.
- `tx-mitigation-credits` — mitigation credit codes by county, counted by code
  rather than banded by percentage.
- `tx-validation-exceptions` — every validation failure from the run, with the
  policy identifier, the rule that failed and the values involved. Not
  submitted to TDI; it exists for the review cycle.

## Run order and dependencies

Reports run in the order listed. Three dependencies matter:

- `tx-premium-summary` establishes the ceded and retained premium split that
  `tx-policy-count` and `tx-catastrophe-exposure` both read, so it runs first
- `tx-catastrophe-exposure` depends on the tier assignment validation having
  run, since it excludes risks whose tier could not be resolved
- `tx-validation-exceptions` runs last by definition, collecting exceptions
  raised by every prior report

A report producing zero rows halts the run. `tx-catastrophe-exposure` produced
zero rows on its first scheduled execution in 2026, before the cession join was
corrected, and the halt is what surfaced the problem.

## Review

All reports are published to the Texas review dashboard in QuickSight.
Reviewers work through `tx-validation-exceptions` first, then the substantive
reports, because an unresolved exception usually changes figures in several
reports at once.

Reconciliation between the ceded and retained sections of
`tx-premium-summary` is the step reviewers spend most time on. It cannot be
skipped: the two sections sum to ledger premium only once the cession is
accounted for, and a reviewer who compares either section to the ledger on its
own will always find a variance.

Submission files are exported from the reviewed QuickSight datasets rather than
regenerated from SQL, so what goes to TDI is exactly what was signed off.
