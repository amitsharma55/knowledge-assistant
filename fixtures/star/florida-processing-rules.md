---
url: https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0020031
updated: 2026-08-09
---
# Florida Processing Rules

How the pipeline transforms raw policy extracts for the Florida data call. These
rules apply only to the Florida package.

## Geographic aggregation

Florida aggregates exposure by county code (FIPS) and by wind-borne-debris
region. Coastal counties are further split by the distance-to-coast band FLOIR
publishes.

## Assumed policies

Policies assumed from Citizens are flagged with `assumption_takeout_id` and
reported in the voluntary package, not the residual-market package.

## Catastrophe

Hurricane exposure is summarized separately from the base premium record, keyed
to the FHCF layer the policy participates in.
