---
url: https://sf-demo.coupahost.com/middleware/sandbox-refresh
updated: 2026-08-08
---
# Sandbox Refresh

How the Coupa sandbox is refreshed from production.

## Behavior

- The sandbox is refreshed on request with supplier bank details masked.
- Integrations must be repointed at sandbox endpoints after a refresh.

## Failure modes

- `SANDBOX-STALE-ENDPOINT` — an integration still points at production after a
  refresh; repoint it before running tests.
