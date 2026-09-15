---
url: https://sf-demo.coupahost.com/middleware/saml-sso
updated: 2026-08-03
---
# SAML SSO

How internal users authenticate to Coupa through the corporate identity
provider over SAML.

## Behavior

- Only service-provider-initiated SSO is supported.
- Just-in-time provisioning is disabled in favor of SCIM.

## Failure modes

- `SAML-ASSERTION-EXPIRED` — clock skew exceeded tolerance; check NTP on the
  identity provider.
