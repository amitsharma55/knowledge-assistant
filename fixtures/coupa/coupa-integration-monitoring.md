---
url: https://sf-demo.coupahost.com/middleware/integration-monitoring
updated: 2026-08-06
---
# Integration Monitoring

How the middleware integrations are monitored and alerted.

## Behavior

- Each integration emits a heartbeat and a success rate to CloudWatch.
- A missed heartbeat pages after two intervals.

## Failure modes

- `MON-NO-HEARTBEAT` — the integration stopped emitting; check the scheduler
  before suspecting the integration itself.
