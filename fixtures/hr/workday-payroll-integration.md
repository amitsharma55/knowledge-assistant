---
url: https://wd5-impl.workday.com/sf_demo/d/payroll-integration
updated: 2026-08-17
---
# Workday Payroll Integration

Moves worker, position and compensation data from Workday into the payroll
system, and returns payroll results for reporting. Workday is the system of
record throughout; payroll never originates a change to worker data.

## What syncs

Outbound to payroll:

- New hires, once the hire business process completes and a start date is set
- Position changes: promotions, transfers and regrades
- Compensation changes, effective-dated so payroll applies them in the correct
  period
- Terminations, including the final working date and termination reason
- Bank and tax elections changed by the worker in Workday

Inbound from payroll:

- Gross-to-net results per pay period, loaded back for reporting only
- Payroll-calculated deductions, which Workday stores but does not act on

A change is only sent once its Workday business process has fully completed.
An in-flight promotion awaiting approval is not visible to payroll, which is
why a manager who has approved a raise may not see it reflected until the
remaining approvals land.

## Jobs

- `workday-payroll-sync` — every 4 hours. Sends completed worker, position and
  compensation changes to payroll.
- `workday-payroll-results` — daily at 05:00 CT. Loads the prior day's payroll
  results back into Workday for reporting.
- `workday-payroll-reconcile` — weekly, Sundays 02:00 CT. Compares active
  positions and compensation in Workday against payroll and reports drift.
- `workday-bank-elections` — every 4 hours, offset from the main sync. Handled
  separately because bank detail changes carry stricter handling requirements.

## Field mapping

Worker and position fields sent on every change:

- `Employee_ID` — the correlation key for every record; payroll's own worker
  number is never used as the key
- `Legal_Name_First`, `Legal_Name_Last` — legal name only; preferred name is
  not sent
- `Hire_Date`, `Termination_Date` — ISO 8601
- `Position_ID` — the position, not the worker, since compensation attaches to it
- `Job_Profile`, `Job_Family`, `Grade` — used by payroll for reporting splits
- `Supervisory_Organization_ID` — for cost centre derivation
- `Cost_Centre` — resolved from the supervisory organization
- `Base_Pay_Amount`, `Pay_Currency`, `Pay_Frequency`
- `Effective_Date` — the date the change takes effect, which may be in the past
  for a retroactive adjustment or in the future for a scheduled increase
- `FLSA_Status` — exempt or non-exempt

Personal contact details, dependents and emergency contacts are never sent.
Payroll has no need for them and the integration is scoped to employment data.

## Retroactive changes

A compensation change with an effective date in a closed pay period is sent
with its original effective date rather than the current one. Payroll
calculates the retroactive adjustment; the middleware does not attempt to
compute or split it. A retroactive change more than two pay periods old is
flagged in the run summary for payroll to review before processing.
