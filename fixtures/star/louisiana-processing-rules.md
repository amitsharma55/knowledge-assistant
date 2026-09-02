# Louisiana Processing Rules

The data elements the Louisiana data call collects and the logic applied to
them. These are set by LDI requirements and are specific to Louisiana: the
pipeline is shared across states, the rules are not.

## Required data elements

Beyond the common exposure elements collected for every state, Louisiana
requires:

- **`parish_code`** — the Louisiana parish of the risk location. Louisiana
  reports by parish, not county, and LDI rejects a submission using county
  identifiers. Assigned from the geocoding service at extract time.
- **`coastal_zone_indicator`** — whether the risk sits inside the coastal zone
  as defined by LDI's published parish list. Drives separate aggregation of
  coastal exposure.
- `fortified_roof_credit` — whether a fortified roof premium credit is applied,
  and at what percentage. LDI tracks uptake of the credit as a mitigation
  measure.
- `citizens_takeout_flag` — whether the policy was taken out of Louisiana
  Citizens, and the takeout term.
- `flood_coverage_source` — whether flood is written through the NFIP, a
  private carrier, or not at all.
- `roof_age_years` — required on residential property; LDI uses it alongside
  the fortified roof credit.

The common elements shared with other states — policy identifier, line of
business, coverage code, written and earned premium, exposure counts and loss
amounts — are described in the data call pipeline document.

## Louisiana Citizens takeouts

Policies taken out of Louisiana Citizens are reported but treated separately
from ordinary new business:

- A takeout policy carries `citizens_takeout_flag` for its first term with us
- It is **excluded** from **voluntary market** new business counts for that
  first renewal term, because counting it would overstate voluntary market
  growth — the policyholder did not choose to move, the depopulation programme
  moved them
- It is included in total exposure counts and in all premium figures throughout
- From the second renewal onward it is ordinary voluntary business and the flag
  is cleared

This exclusion applies only to the count of new voluntary policies. Premium,
loss and exposure figures always include takeout policies, and a reviewer
comparing the two will see the counts and premium diverge for that reason.

## Coastal zone aggregation

Exposure inside the coastal zone is aggregated separately from inland exposure:

- Coastal and inland totals are reported as distinct rows, never combined
- The parish list defining the coastal zone comes from LDI and is refreshed
  each year before the January extract; a parish moving between zones changes
  year-over-year comparability, and the report notes it when it happens
- Where a policy's geocode is ambiguous, it is assigned to coastal, which is
  the conservative treatment LDI expects

## Validation rules

Validation produces an exceptions dataset rather than failing the run:

- Every risk must have a valid `parish_code` from the current LDI parish list;
  an unassigned or retired parish is an exception
- Coastal zone indicator must be consistent with the parish's zone assignment
- Fortified roof credit must be absent where `roof_age_years` is null
- Total premium by parish must reconcile to the ledger within $1,000; a larger
  variance is an exception and blocks sign-off
- Loss history must cover three full years; a policy with less is reported but
  flagged
