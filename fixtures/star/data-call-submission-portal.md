---
url: https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0020060
updated: 2026-08-26
---
# Data Call Submission Portal

How completed data-call packages are delivered to each regulator once the
pipeline has produced them. The portal is common to every state; the package
contents are not.

## Delivery

- Each state package is uploaded as a signed zip to that regulator's SFTP
  endpoint, then acknowledged with a filing receipt id.
- Receipt ids are stored against the run so a resubmission can reference the
  original filing.

## Failure modes

- `PORTAL-SFTP-REJECT` — the regulator endpoint refused the connection; check
  the endpoint's published maintenance window before retrying.
