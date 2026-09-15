---
url: https://wd5-impl.workday.com/sf_demo/d/benefits-enrollment
updated: 2026-08-06
---
# Benefits Enrollment

How benefits elections are made and processed in Workday. This covers health,
dental, and retirement elections; time off is handled by the leave policy.

## Open enrollment

- The annual window runs for three weeks each November. Elections made in the
  window take effect January 1.
- A qualifying life event opens a 30-day special enrollment outside the window.

## Processing

Elections are validated against plan eligibility rules and, once confirmed,
sent to each carrier in the nightly benefits feed. A failed carrier feed is
retried the next night; it does not block payroll.
