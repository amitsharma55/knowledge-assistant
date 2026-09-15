---
url: https://sf-demo.coupahost.com/middleware/supplier/portal-provisioning
updated: 2026-08-21
---
# Supplier Portal Provisioning

Creates and manages supplier logins to the Coupa Supplier Portal, including SSO
federation for large suppliers.

## Provisioning

- A new supplier contact is invited by email; SSO-federated suppliers are
  matched on their identity provider domain.

## Failure modes

- `PORTAL-DOMAIN-UNCLAIMED` — the supplier's email domain is not federated;
  fall back to password login.
