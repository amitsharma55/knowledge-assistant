# Texas Data Call

The Texas statistical data call, submitted annually to the **Texas Department
of Insurance (TDI)**. Covers personal residential property and personal auto
exposure written in Texas.

The pipeline that produces it is shared across states and described in the data
call pipeline document. The data elements TDI requires and the rules applied to
them are specific to Texas and live in the Texas processing rules document.

## Scope

- Covers all policies with a Texas risk location, regardless of where the
  policyholder resides
- Personal residential property and personal auto only; commercial lines are
  reported through a separate TDI filing that this data call does not feed
- Reporting period is the prior calendar year, January 1 to December 31
- Surplus lines business is out of scope and excluded at extract
- Exposure ceded to the windstorm pool is in scope but reported separately, as
  described in the Texas processing rules

## Submission deadline

The Texas data call is submitted to the TDI by **May 15** each year, covering
the prior calendar year.

The internal schedule works backwards from that date:

- **March 1** — reporting period snapshot taken and extracts run
- **April 1** — all reports generated and published to QuickSight for review
- **May 1** — review complete, exceptions resolved or documented
- **May 15** — submission to TDI

The later deadline gives more slack than other filings, and it is usually
enough. The exception is a year with significant catastrophe activity, when
reconciling the catastrophe exposure figures takes longer than the schedule
allows and the April 1 milestone slips.

## Data sources

- Policy and premium data from the Redshift warehouse for current terms
- Historical terms from the mainframe extract, needed because TDI requires five
  years of loss history alongside current exposure
- County assignments from the geocoding service, applied at extract time
- Windstorm pool cession data from the reinsurance system, which is not in the
  warehouse and is joined during staging
- Catastrophe loss coding from the claims warehouse, used to separate named
  storm losses from other perils

## Contacts

- Business owner: the State Filings team, who own the relationship with TDI and
  sign off the submission
- Technical owner: Star, who own the pipeline and the report SQL
- Questions about what TDI is asking for go to State Filings; questions about
  how a number was produced go to Star
- TDI's own bulletins are the authority on requirements; where this
  documentation and a bulletin disagree, the bulletin wins and this
  documentation is wrong
