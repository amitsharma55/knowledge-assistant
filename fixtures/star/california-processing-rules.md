---
url: https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0020041
updated: 2026-08-13
---
# California Processing Rules

Transformation rules for the California data call package only.

## Geographic aggregation

California aggregates by county code (FIPS) and by CDI-defined wildfire risk
tier. The wildfire tier is derived from the state fire-hazard severity map, not
supplied by the carrier.

## Wildfire mitigation

Policies with a verified home-hardening credit set a `mitigation_credit_flag`,
which the CDI package reports alongside the base premium.
