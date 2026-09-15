---
url: https://sf-demo.coupahost.com/middleware/identity/scim-provisioning
updated: 2026-09-01
---
# SCIM User Provisioning

Provisions and deprovisions internal Coupa users from the corporate identity
provider over SCIM. Distinct from supplier portal logins.

## Behavior

- A joiner in the IdP creates a Coupa user with role mappings from group
  membership.
- A leaver is deactivated within one sync cycle.
