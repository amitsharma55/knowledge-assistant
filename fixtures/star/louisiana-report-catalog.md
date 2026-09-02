# Louisiana Report Catalog

The reports the Louisiana data call produces, in the order they run. Each is a
single SQL file executed against the staged parquet, writing one output dataset
that is published to QuickSight for review before submission to LDI.

Report naming is prefixed `la-` throughout, so Louisiana and other states'
outputs cannot be confused in S3 or in QuickSight.

## Reports

The Louisiana data call produces seven reports, all prefixed `la-`:

- **`la-premium-summary`** — written and earned premium by line of business,
  coverage code and parish. The headline report and the one LDI reviews first.
  Split into coastal and inland sections, which are never combined into a
  single total.
- **`la-loss-detail`** — paid and incurred losses by parish and coverage, with
  catastrophe losses separated from attritional. Carries three years of loss
  history, which is what the mainframe extract exists to supply.
- `la-policy-count` — in-force policy counts by parish and line, with new
  voluntary business shown separately from Louisiana Citizens takeouts.
- `la-coverage-breakdown` — exposure by coverage code and limit band, used by
  LDI to assess concentration.
- `la-mitigation-credits` — fortified roof credit uptake by parish, with credit
  percentages banded. Added at LDI's request to track mitigation adoption.
- `la-flood-coverage` — flood coverage source split across NFIP, private and
  none, by parish.
- `la-validation-exceptions` — every validation failure from the run, with the
  policy identifier, the rule that failed and the values involved. Not
  submitted to LDI; it exists for the review cycle.

## Run order and dependencies

Reports run in the order listed. Three dependencies matter:

- `la-policy-count` reads the takeout classification that `la-premium-summary`
  establishes, so it cannot run first
- `la-mitigation-credits` depends on the roof age validation having run, since
  it excludes policies flagged for a missing roof age
- `la-validation-exceptions` runs last by definition, collecting exceptions
  raised by every prior report

A report producing zero rows halts the run. For `la-mitigation-credits` this
has happened legitimately in the past — in the first year the credit existed,
uptake genuinely was zero for two parishes — and the run was overridden
manually with sign-off from State Filings.

## Review

All reports are published to the Louisiana review dashboard in QuickSight.
Reviewers work through `la-validation-exceptions` first, then the substantive
reports, because an unresolved exception usually changes figures in several
reports at once.

Submission files are exported from the reviewed QuickSight datasets rather than
regenerated from SQL, so what goes to LDI is exactly what was signed off.
