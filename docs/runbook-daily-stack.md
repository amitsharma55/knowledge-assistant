# Runbook — Daily AWS Stack (up / down)

Operational guide for the disposable daily environment: EKS + OpenSearch (the
`terraform/daily` stack, "4a") plus the app workloads on it (`deploy/k8s`, "4b"),
sequenced by `deploy/k8s/day.sh` ("4c"). The stack is meant to live for one
working session and be torn down each evening — never leave it applied overnight.

Persistent state (S3 corpus, IAM roles, ECR repos, the Anthropic API secret)
lives in the **foundation** stack and survives teardown; the daily stack rebuilds
its OpenSearch index from S3 on every bring-up.

## Prerequisites (once per laptop)

- Tools on `PATH`: `docker`, `terraform`, `aws`, `helm`, `kubectl`, `jq`, `make`
  (`day.sh` preflights these).
- AWS credentials for the account, in the shell you run `day.sh` from.
- The daily stack initialized against its S3 backend (leaves
  `terraform/daily/.terraform/terraform.tfstate`). If missing, `day.sh up` prints
  the exact steps:
  ```sh
  cd terraform/daily
  cp backend.hcl.example backend.hcl                 # edit values
  cp private.auto.tfvars.example private.auto.tfvars # edit values
  terraform init -backend-config=backend.hcl
  ```
- The Anthropic API key stored in the foundation secret (once; it persists):
  ```sh
  aws secretsmanager put-secret-value \
    --secret-id <anthropic_secret_name> --secret-string 'sk-ant-...'
  ```
  A JSON object (`{"ANTHROPIC_API_KEY":"sk-ant-..."}`) is also accepted — chat-api
  parses either form (`internal/secrets`).

## Bring-up

```sh
deploy/k8s/day.sh up
```

This runs, in order:

1. **`make images` ‖ `terraform apply`** — image builds run in parallel with the
   stack apply (images push to foundation's ECR; a failed apply kills the build).
2. **`verify.sh`** — checks the cluster is ACTIVE, add-ons installed, node group
   ACTIVE at desired size, and the OpenSearch domain is Active. The OpenSearch
   check **polls up to 20 minutes** (domain creation is slow), so this step can
   sit waiting — that is normal, not a hang. Tune with `VERIFY_OS_TIMEOUT` /
   `VERIFY_OS_INTERVAL` (seconds) if needed.
3. **`deploy.sh up latest`** — helm-installs the ALB controller, applies the
   kustomize overlay, runs a fresh `reseed` Job (rebuilds the OpenSearch index
   from S3), restarts chat-api/ui so they pick up the new images and config, then
   prints the ALB URL.

The **ALB URL changes on every cycle** — always take it from the tail of
`day.sh up` (or `kubectl -n knowledge-assistant get ingress ka`).

Smoke-test a live deploy: `deploy/k8s/smoke.sh`.

## Keep it up past the reaper

A LaunchAgent runs `day.sh reap` at 22:00 local and tears the stack down unless a
keep-flag is set for today. To keep it for a late demo:

```sh
deploy/k8s/day.sh keep      # touches ~/.ka/keep-<today>; reaper skips teardown
```

Reaper logs and keep-flags live under `~/.ka/` (`KA_STATE_DIR`).

## Teardown

```sh
deploy/k8s/day.sh down
```

Order matters and `day.sh down` enforces it:

1. **`deploy.sh down`** deletes the Ingress and **waits** — the ALB controller's
   finalizer must delete the real ALB and its ENIs first, or the next step hangs
   on subnet/ENI dependencies.
2. **`terraform destroy`** on the daily stack only (foundation is never touched).

OpenSearch domain deletion is slow (~10–15 min) — expected, not a hang. After a
clean down, billing returns to ~zero.

## Troubleshooting

| Symptom | Cause / fix |
| --- | --- |
| `verify.sh` sits on OpenSearch for many minutes | Normal — domain still Creating/Modifying; it polls up to 20 min. Only a genuine stall past that is a failure. |
| `terraform destroy` hangs on subnets/ENIs | The Ingress/ALB wasn't released first. Run `deploy/k8s/deploy.sh down`, then retry destroy. `day.sh down` sequences this for you. |
| Grounded queries 502 / auth errors | OpenSearch requests must be SigV4-signed for the AWS domain; embed/query vectors must come from one model. Check chat-api logs. |
| chat-api won't start: "no key" | `KA_LLM_MODE=anthropic` but the secret is empty/misnamed. Verify `put-secret-value` and `KA_ANTHROPIC_SECRET_ID`. |
| Pods didn't pick up a new image/config | `deploy.sh up` now `rollout restart`s chat-api/ui automatically; if you applied manually, run `kubectl -n knowledge-assistant rollout restart deploy/chat-api deploy/ui`. |
| Blank UI over plain HTTP | Known: `crypto.randomUUID` needs a secure context; already handled in the UI bundle. |

## Offline gates (no AWS, no cluster)

- `make k8s-check` — renders manifests, `bash -n`, and runs the deploy/day/reaper
  script tests.
- `make tf-check` — terraform fmt/validate + `terraform test` (mocked provider).

Run these before pushing changes to the deploy or terraform assets.
