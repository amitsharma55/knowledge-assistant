---
url: https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0020006
updated: 2026-08-29
---
# Texas Processing Rules

The data elements the Texas data call collects and the logic applied to them.
These are set by TDI requirements and are specific to Texas: the pipeline is
shared across states, the rules are not.

## Required data elements

Beyond the common exposure elements collected for every state, Texas requires:

- **`county_fips`** — the five-digit FIPS code for the county of the risk
  location. TDI requires FIPS rather than county names, and a submission using
  names is rejected. Assigned from the geocoding service at extract time.
- **`windstorm_pool_indicator`** — whether the wind peril on the risk is ceded
  to the windstorm pool. Drives the separate cession reporting described below.
- `tier_1_coastal_county` — whether the risk sits in a tier-1 coastal county as
  defined by TDI's published list. Distinct from the windstorm pool indicator:
  a risk can be in a tier-1 county without being ceded.
- `mitigation_credit_code` — the specific mitigation credit applied, from TDI's
  code list. TDI collects the code rather than a percentage.
- `roof_covering_type` — required on residential property, from TDI's
  materials list.
- `named_storm_deductible_pct` — the named storm deductible as a percentage of
  Coverage A, where one applies.

The common elements shared with other states — policy identifier, line of
business, coverage code, written and earned premium, exposure counts and loss
amounts — are described in the data call pipeline document.

## TWIA-ceded exposure

Exposure ceded to the **Texas Windstorm Insurance Association (TWIA)** is
reported separately from retained exposure, and the separation runs through
every report:

- A risk with `windstorm_pool_indicator` set has its wind exposure **ceded** to
  TWIA and its other perils retained. The same policy therefore contributes to
  both the ceded and retained figures, for different perils.
- Ceded and retained exposure are **reported separately** and never summed into
  a combined total. TDI reviews them as distinct populations.
- Tier-1 coastal counties are aggregated on their own, separately again from
  the ceded/retained split, so TWIA-ceded wind exposure in a tier-1 county is
  never mixed into either the retained totals or the non-tier-1 coastal
  figures.
- Premium follows the peril, not the policy: the wind portion of premium on a
  ceded risk is reported as ceded, and the remainder as retained. The split
  comes from the reinsurance system, not from the policy record.

This is the single most common source of reconciliation questions on this data
call, because total premium across the ceded and retained reports does not
equal ledger premium until the cession is accounted for.

## Validation rules

Validation produces an exceptions dataset rather than failing the run:

- Every risk must have a valid five-digit `county_fips` from the current TDI
  list; an unassigned or retired code is an exception
- `tier_1_coastal_county` must be consistent with the county's TDI tier
  assignment
- A risk with `windstorm_pool_indicator` set must have a matching cession
  record in the reinsurance system; a missing one blocks sign-off, because the
  premium split cannot be derived without it
- `named_storm_deductible_pct` must be present wherever the risk is in a tier-1
  county
- Ceded plus retained premium must reconcile to ledger premium within $1,000
- Loss history must cover five full years; a policy with less is reported but
  flagged

## Changelog

- **2026-06** — Catastrophe exposure reporting added, introducing the
  `tx-catastrophe-exposure` report and the `named_storm_deductible_pct` element,
  in response to a TDI reporting change following the prior season.
- **2026-01** — `mitigation_credit_code` replaced a free-text mitigation field
  after TDI published its code list.
- **2025-09** — Initial release covering personal residential property and
  personal auto.
