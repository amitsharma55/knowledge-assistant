# AVR Integration

**AVR (Automated Vulnerability Response)** ingests security findings from
scanners (Wiz, Snyk, AWS Inspector, Semgrep), normalizes them, and dispatches
remediation actions. Runs entirely on AWS Lambda in region `us-east-1` under
account `acme-security-prod`.

## Lambda functions

| Lambda                          | Runtime        | Trigger                              | Purpose                                                    |
|---------------------------------|----------------|--------------------------------------|------------------------------------------------------------|
| `avr-ingest-wiz`                | Python 3.13    | EventBridge from Wiz webhook         | Normalizes Wiz findings into the AVR schema                |
| `avr-ingest-snyk`               | Python 3.13    | SQS from Snyk webhook receiver       | Normalizes Snyk vulnerabilities                            |
| `avr-ingest-inspector`          | Python 3.13    | EventBridge (AWS Inspector v2)       | Ingests container + EC2 findings                           |
| `avr-ingest-semgrep`            | Node.js 24     | GitHub webhook via API Gateway       | Ingests SAST findings on PR events                         |
| `avr-normalize`                 | Python 3.13    | Kinesis (`avr-raw` stream)           | Deduplicates and enriches findings; writes to `avr-store`  |
| `avr-router`                    | Go 1.24        | DynamoDB stream on `avr-store`       | Routes new findings to the correct action lambda           |
| `avr-action-jira`               | Node.js 24     | SQS `avr-actions-jira`               | Opens Jira tickets for team-owned findings                 |
| `avr-action-quarantine-ec2`     | Python 3.13    | SQS `avr-actions-quarantine`         | Applies isolate-security-group tag to compromised EC2s     |
| `avr-action-revoke-iam-key`     | Python 3.13    | SQS `avr-actions-iam`                | Revokes leaked IAM access keys via IAM API                 |
| `avr-action-block-egress`       | Go 1.24        | SQS `avr-actions-network`            | Adds deny rules to the egress firewall for known-bad IPs   |
| `avr-slack-notifier`            | Node.js 24     | SNS `avr-notifications`              | Posts summaries to `#security-alerts`                      |
| `avr-daily-digest`              | Python 3.13    | EventBridge cron `0 14 * * *`        | Rolls up open findings by service owner; emails leads      |
| `avr-dlq-replay`                | Python 3.13    | Manual invoke / CloudWatch alarm     | Re-processes failed events from `avr-dlq`                  |

## Typical invocation volume (rolling 24h)

- `avr-ingest-*`: 8k–15k invocations combined
- `avr-normalize`: 1:1 with ingest — currently 12k/day
- `avr-router`: ~11k (some duplicates dropped upstream)
- Action lambdas: ~600 total (most findings are informational and don't
  auto-remediate)
- Cold starts: <2%, kept low by provisioned concurrency of 2 on the ingest
  and normalize functions

## Data stores

- `avr-store` — DynamoDB table, single-item-per-finding, TTL 180 days
- `avr-raw` — Kinesis stream, 5-day retention
- `avr-dlq` — SQS DLQ for any lambda failure after 3 retries

## Escalation

If `avr-normalize` DLQ depth exceeds 100 messages, PagerDuty pages the
Security Platform on-call. Runbook lives at
`https://wiki.internal/security/avr-runbook`.
