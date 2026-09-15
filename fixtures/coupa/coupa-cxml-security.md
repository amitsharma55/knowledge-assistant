---
url: https://sf-demo.coupahost.com/middleware/cxml-security
updated: 2026-08-07
---
# cXML Security

How cXML documents exchanged with suppliers are secured.

## Behavior

- Each cXML document carries a shared secret in the credential block.
- Secrets rotate on a schedule held in Secrets Manager.

## Failure modes

- `CXML-BAD-CREDENTIAL` — the shared secret did not match; rotate and resend.
