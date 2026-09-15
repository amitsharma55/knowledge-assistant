---
url: https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0020002
updated: 2026-08-01
---
# Louisiana Data Call

The Louisiana statistical data call, submitted annually to the **Louisiana
Department of Insurance (LDI)**. Covers personal residential property and
personal auto exposure written in Louisiana.

The pipeline that produces it is shared across states and described in the data
call pipeline document. The data elements Louisiana requires and the rules
applied to them are Louisiana-specific and live in the Louisiana processing
rules document.

## Scope

- Covers all policies with a Louisiana risk location, regardless of where the
  policyholder resides
- Personal residential property and personal auto only; commercial lines are
  reported through a separate LDI filing that this data call does not feed
- Reporting period is the prior calendar year, January 1 to December 31
- Surplus lines business is out of scope and excluded at extract

## Submission deadline

The Louisiana data call is submitted to the LDI by **March 1** each year,
covering the prior calendar year.

The internal schedule works backwards from that date:

- **January 15** — reporting period snapshot taken and extracts run
- **February 1** — all reports generated and published to QuickSight for review
- **February 15** — review complete, exceptions resolved or documented
- **March 1** — submission to LDI

The February 15 milestone is the one that slips. Validation exceptions on
parish-level exposure are the usual cause, because resolving them often needs
the underwriting team rather than the data team.

## Data sources

- Policy and premium data from the Redshift warehouse for current terms
- Historical terms from the mainframe extract, needed because LDI requires
  three years of loss history alongside current exposure
- Parish assignments from the geocoding service, applied at extract time rather
  than read from the policy record, since older policies predate parish coding
- Catastrophe loss coding from the claims warehouse, used to separate
  hurricane-related losses from attritional ones

## Contacts

- Business owner: the State Filings team, who own the relationship with LDI and
  sign off the submission
- Technical owner: Star, who own the pipeline and the report SQL
- Questions about what LDI is asking for go to State Filings; questions about
  how a number was produced go to Star
- LDI's own bulletins are the authority on requirements; where this
  documentation and a bulletin disagree, the bulletin wins and this
  documentation is wrong
