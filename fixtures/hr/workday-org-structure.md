---
url: https://wd5-impl.workday.com/sf_demo/d/org-structure
updated: 2026-07-28
---
# Workday Organization Structure

How the organization is modelled in **Workday**, which is the system of record
for worker data, reporting lines and positions. Everything downstream — payroll,
access provisioning, the org chart — reads this structure rather than
maintaining its own.

## Supervisory organizations

The **supervisory organization** holds the reporting line. A worker's manager
is the manager of the supervisory organization the worker sits in, not a field
on the worker record.

- Every worker belongs to exactly one supervisory organization
- Each supervisory organization has exactly one manager, and that manager sits
  in the *parent* supervisory organization, not their own
- Supervisory organizations nest to form the reporting hierarchy; the top of
  the tree is the CEO's organization
- A manager with direct reports and a manager of managers are the same
  structure at different depths; there is no separate concept

The consequence that surprises people: changing someone's manager means moving
them to a different supervisory organization, which is a business process with
approvals, not a field edit. This is also why a reporting line change can lag a
verbal one by days.

Subordinate organization types exist for matrixed work — project teams and
committees — but they carry no reporting authority and payroll ignores them
entirely.

## Worker records

- Every worker has a single `Employee_ID`, stable for life, which survives
  termination and rehire
- Contingent workers have a `Contingent_Worker_ID` in a separate sequence, and
  are never assigned an `Employee_ID` even if later converted to permanent
- A worker record carries legal name, preferred name, work contact details and
  the supervisory organization; compensation lives on the position, not the
  worker
- Personal contact details and dependents are restricted and are not exposed to
  any downstream integration

## Position management

The organization runs position management, meaning a worker is hired into a
defined position rather than simply into an organization:

- A position exists independently of whoever fills it, and carries the job
  profile, job family, grade and location
- A vacant position remains in the supervisory organization and is what
  recruiting fills; headcount is counted in positions, not workers
- Compensation bands attach to the position's grade and job family, not to the
  individual
- A promotion is a change of position, which is why it changes the compensation
  band that applies

## What downstream systems read

- Payroll reads positions, compensation and the supervisory organization
- Access provisioning reads the supervisory organization to derive approval
  chains
- The org chart is generated from supervisory organizations nightly
- No downstream system reads worker personal data; every integration is scoped
  to employment data only
