---
url: https://sf-demo.coupahost.com/middleware/architecture
updated: 2026-08-19
---
# Coupa AWS Middleware Architecture

How the three Coupa integrations — ServiceNow, Icertis and AVR — fit together
on AWS. Each is independently deployed and independently failable; they share
infrastructure but no runtime state, so an incident on one does not stop the
others.

## AWS footprint

Everything runs in **`us-east-1`** in the AWS account
`sf-coupa-middleware-prod`. The non-production account is
`sf-coupa-middleware-nonprod`, in the same region, with its own Coupa sandbox
tenant.

- Compute is AWS Lambda throughout; there are no long-running services
- Scheduled work is driven by EventBridge rules, one per job
- Queue-driven work uses SQS, with a dead letter queue per integration
- Secrets live in Secrets Manager under the `coupa-middleware/` prefix
- All Lambdas run in VPC subnets with a route to on-prem over Direct Connect,
  which only the AVR integration actually uses

## The three integrations

- **ServiceNow** is bidirectional and scheduled. `snow-product-push` sends
  catalog content to ServiceNow; `snow-request-pull` brings approved requests
  back into Coupa. Both are EventBridge-triggered and hold their own watermarks.
- **Icertis** is a three-phase pipeline — fetch, load, resync — with each phase
  a separate Lambda on its own schedule, staging contracts in S3 between them.
  It is the only integration that persists intermediate state.
- **AVR** is request/response. It holds no state and runs no schedule: Coupa
  calls it synchronously and it calls the on-prem SOAP service in turn.

The asymmetry is deliberate. ServiceNow and Icertis move data on a schedule and
must survive the other system being down, so they queue and retry. AVR answers
questions in real time, so it fails fast instead.

## Shared components

- **API Gateway** fronts every inbound HTTP path, currently only the AVR REST
  facade. It terminates TLS, applies the per-consumer usage plan and forwards
  to Lambda.
- **Direct Connect** provides the only route to on-prem systems. There is no
  VPN fallback, so a circuit failure takes AVR down entirely.
- **A shared correlation identifier**, `x-request-id`, is generated at the edge
  and carried through every log line, so a single Coupa request can be traced
  across Lambdas.
- **CloudWatch log groups** are named per component: `/aws/lambda/snow-sync`,
  `/aws/lambda/icertis-*` and `/aws/lambda/avr-facade`.

## Data stores

- `s3://sf-coupa-middleware/icertis/staged/` — contracts fetched from Icertis
  awaiting load. Lifecycle rule expires objects after 30 days.
- `snow-sync-watermarks` — DynamoDB table holding the last successful sync
  position per ServiceNow content group.
- `icertis-load-state` — DynamoDB table tracking which phase each contract has
  reached, and the Coupa contract identifier once loaded.
- SQS dead letter queues: `snow-product-dlq`, `snow-request-dlq`,
  `icertis-load-dlq`. AVR has none, because a failed AVR request is returned to
  Coupa rather than retained.

No integration reads another's store. The only coupling is that all three
report into the same CloudWatch dashboard and the same PagerDuty service.
