# AWS Permanent Foundation

Date: 2026-09-10 (revised 2026-09-11)
Status: Approved for planning

## Problem

The assistant runs only on a laptop: Docker Compose for OpenSearch and Ollama,
chat-api and the UI as host processes. The next step is a production-shaped
pre-production environment on the owner's personal AWS account, created at the
start of each working day and destroyed at the end. That costs roughly
$0.28/hour while it is up — about $60/month — instead of about $230/month
always-on.

A daily environment needs somewhere to stand: a home for Terraform state, the
container images, the document corpus and the Claude API key; networking that is
reused every morning; and the IAM roles the daily cluster and its pods assume.
None of it exists. The account has no project buckets or repositories, and it
lacks the OpenSearch service-linked role that a VPC domain requires. Terraform on
the owner's machine is 1.5.7 (2023), and other projects depend on it.

This spec covers that permanent layer. It is sub-project 2 of four:

1. Bedrock model clients: Titan Text Embeddings v2, gpt-oss-20b rerank and rewrite.
2. **Permanent foundation — this spec.**
3. S3 ingestion source.
4. Daily stack, applied each morning and destroyed each evening from the laptop.

## Goals

- Every resource that must survive the nightly teardown exists, is defined in
  Terraform, and together costs about $0.50/month.
- The daily stack can be written against the foundation's outputs and creates no
  IAM, so the nightly `terraform destroy` never touches IAM.
- Every value that could vary — region, names, CIDRs, availability zones, limits,
  retention periods, model IDs, email addresses — comes from tfvars. The `.tf`
  files contain no literal configuration.
- Nothing that identifies the account or the owner is committed to the public
  repository.
- Spend is visible before the bill is: an account-wide budget alerts by email.

## Non-Goals

- The daily stack — EKS cluster, node group, OpenSearch domain, load balancer
  controller — the image build and push, the morning/evening commands, and the
  safety net for a forgotten teardown (sub-project 4).
- CI/CD. Terraform runs only from the owner's laptop, and GitHub Actions has no
  access to the account. Adding CI later means adding a GitHub OIDC provider and
  a scoped deploy role; nothing in this spec would need to change.
- Application code: the Bedrock clients and chat-api reading the key from Secrets
  Manager (sub-project 1); the indexer reading S3 (sub-project 3).
- A stable DNS name and TLS certificate. The account has a Route 53 hosted zone;
  revisit in sub-project 4.
- Budget actions that stop resources automatically.
- Pre-existing resources in the account, including its existing $5
  "Monthly Budget". The foundation creates and manages only its own resources.
- Multiple accounts or regions, and production high availability.

## Decisions

Settled with the owner; do not relitigate without new information.

- **Terraform runs only from the laptop.** The owner applies bootstrap and the
  foundation once, and later applies the daily stack each morning and destroys it
  each evening, all as the IAM user `Amit_Terraform`. There is no GitHub OIDC
  provider and no deploy role, so nothing on the public repository can reach the
  account. The cost is that no CI job can tear down a forgotten environment;
  sub-project 4 provides a local safety net, and the budget alerts back it up.
- **All configuration comes from tfvars.** Every stack declares each tunable value
  as a typed variable with a description, validation where it helps, and **no
  default**, so a missing value fails at `plan` instead of silently using a
  built-in. Values live in two files per stack:
  - `terraform.tfvars` — committed; everything that does not identify the owner.
  - `private.auto.tfvars` — gitignored; the alert email and `allowed_account_ids`.

  A few things are deliberately not variables:
  - The `backend` block cannot read variables, so its values come from a
    gitignored `backend.hcl` passed to `terraform init -backend-config`.
  - AWS-defined constants, which are facts about AWS rather than settings:
    service principals (`eks.amazonaws.com`), AWS managed policy ARNs, IAM action
    names, the `kubernetes.io/role/elb` subnet tag, OpenSearch's HTTPS port 443,
    the `0.0.0.0/0` internet route, and the budget currency `USD`.
  - The security baseline this spec fixes rather than configures: SSE-S3
    encryption, public access blocks, TLS-only bucket policies, bucket versioning,
    and the VPC DNS settings EKS requires.
  - `prevent_destroy`, which Terraform requires to be a literal.
- **Wrong-account guard.** The provider's `allowed_account_ids` comes from
  `private.auto.tfvars`, so Terraform refuses to run if other credentials are
  active.
- **A top-level `terraform/` folder** holds every stack: `bootstrap` (local state,
  creates only the state bucket), `foundation` (state in that bucket), and later
  `daily`.
- **Terraform 1.16 via tfenv.** Homebrew's `terraform` formula is frozen at 1.5.7
  and conflicts with tfenv, so it is replaced by tfenv. 1.5.7 becomes tfenv's
  global default, so projects without a `.terraform-version` are unchanged; this
  repository pins `1.16.2`.
- **S3-native state locking** (`use_lockfile = true`). No DynamoDB table.
- **The foundation owns all IAM, and pods use EKS Pod Identity.** Pod roles trust
  `pods.eks.amazonaws.com` rather than a particular cluster, so they survive the
  cluster being recreated every morning, mornings never wait for new IAM to take
  effect, and the nightly destroy never deletes IAM. IRSA was rejected because
  its trust policies name the cluster's OIDC issuer URL, which changes daily.
- **The daily stack reads the foundation's outputs with `terraform_remote_state`.**
  Both run locally with the same credentials, so no SSM indirection is needed.
- **Budget: $50/month, account-wide.** Other spend in the account is about
  $1.50/month (Route 53), and tracking the whole account catches orphaned
  resources that may not carry project tags. At the expected ~$60/month the 100%
  alert will usually fire; the owner chose that as a monthly prompt.
- **Region us-east-1.** Titan Text Embeddings v2 and gpt-oss-20b are both
  available on demand there, and `Amit_Terraform` can already invoke both.
- **OpenSearch is reachable only inside the VPC,** behind a security group, with an
  open domain access policy. The existing OpenSearch client, which does not sign
  requests, keeps working. Request signing can be added later without changing
  the foundation.

## Layout

```
.terraform-version                1.16.2
terraform/
  check.sh  check-literals.sh     offline checks, run by `make tf-check`
  tests/                          tests for the check scripts
  modules/
    private-bucket/               hardened S3 bucket, used by both stacks
  bootstrap/
    versions.tf  main.tf  variables.tf  outputs.tf
    terraform.tfvars              committed
    private.auto.tfvars           gitignored
    private.auto.tfvars.example
    tests/
  foundation/
    versions.tf  backend.tf  data.tf  variables.tf  outputs.tf
    network.tf  storage.tf  secrets.tf  iam.tf  budget.tf
    policies/aws-lb-controller-v3.5.0.json
    terraform.tfvars              committed
    private.auto.tfvars           gitignored
    private.auto.tfvars.example
    backend.hcl                   gitignored
    backend.hcl.example
    verify.sh
    tests/
  daily/                          sub-project 4
```

- Version constraints: Terraform `~> 1.16`, `hashicorp/aws ~> 6.64`.
  `.terraform.lock.hcl` is committed.
- Provider `default_tags` are `var.tags`; each stack's `terraform.tfvars` sets its
  own `Stack` tag.
- Every name is built from `var.name_prefix`. Names that must be globally unique
  get the account ID appended at apply time, from `aws_caller_identity`, so the
  account ID never appears in a committed file.
- The security baseline for both buckets (encryption, public access block,
  TLS-only policy, versioning, noncurrent-version expiry, `prevent_destroy`) lives
  once, in `modules/private-bucket`.
- Gitignored: `.terraform/`, `*.tfstate`, `*.tfstate.*`, `*.tfplan`,
  `private.auto.tfvars`, `backend.hcl`.
- `make tf-check` runs every offline check: `terraform fmt`, `validate`,
  `terraform test` against a mocked AWS provider, the no-literals check, and the
  shell script tests. It needs no AWS credentials.

### Variables

Committed values are in `terraform.tfvars`; values marked *private* are in
`private.auto.tfvars`.

**Both stacks**

| Variable | Type | Value |
|---|---|---|
| `aws_region` | string | `us-east-1` |
| `allowed_account_ids` | list(string) | *private* |
| `name_prefix` | string | `ka` |
| `tags` | map(string) | `Project = knowledge-assistant`, `ManagedBy = terraform`, `Stack = bootstrap` or `foundation` |

**Bootstrap**

| Variable | Type | Value |
|---|---|---|
| `state_noncurrent_version_days` | number | `90` |

**Foundation**

| Variable | Type | Value |
|---|---|---|
| `vpc_cidr` | string | `10.40.0.0/16` |
| `subnet_count` | number | `2` |
| `subnet_newbits` | number | `4` (a `/20` per subnet) |
| `excluded_az_ids` | list(string) | `["use1-az3"]` — no EKS control plane there |
| `opensearch_ingress_cidrs` | list(string) | `["10.40.0.0/16"]` |
| `docs_noncurrent_version_days` | number | `30` |
| `ecr_repositories` | set(string) | `knowledge-assistant/chat-api`, `knowledge-assistant/ui`, `knowledge-assistant/ingestion` |
| `ecr_image_tag_mutability` | string | `MUTABLE` |
| `ecr_scan_on_push` | bool | `true` |
| `ecr_keep_tagged_images` | number | `10` |
| `ecr_untagged_expiry_days` | number | `1` |
| `anthropic_secret_name` | string | `ka/anthropic-api-key` |
| `secret_recovery_window_days` | number | `7` |
| `chat_api_bedrock_model_ids` | list(string) | `amazon.titan-embed-text-v2:0`, `openai.gpt-oss-20b-1:0` |
| `ingestion_bedrock_model_ids` | list(string) | `amazon.titan-embed-text-v2:0` |
| `lb_controller_policy_file` | string | `policies/aws-lb-controller-v3.5.0.json` |
| `budget_limit_usd` | number | `50` |
| `budget_time_unit` | string | `MONTHLY` |
| `budget_actual_thresholds_pct` | list(number) | `[50, 80, 100]` |
| `budget_forecast_thresholds_pct` | list(number) | `[100]` |
| `alert_emails` | list(string) | *private* |

## Bootstrap stack

One bucket, `<name_prefix>-tfstate-<account-id>`:

- versioning on, with noncurrent versions kept for `state_noncurrent_version_days`;
- SSE-S3 encryption; all public access blocked;
- a bucket policy that denies requests not made over TLS;
- `lifecycle { prevent_destroy = true }`.

It outputs the bucket name, which the owner copies into
`foundation/backend.hcl`. Losing bootstrap's local state is harmless: the bucket
still exists and can be imported.

## Foundation stack

State: `<state-bucket>/foundation/terraform.tfstate`, with its lock file
alongside.

### Storage and images

- **`<name_prefix>-docs-<account-id>`** holds the document corpus, one prefix per
  team (`coupa/`, `star/`, `hr/`). Versioning on, noncurrent versions expire after
  `docs_noncurrent_version_days`; SSE-S3; public access blocked; TLS-only policy;
  `prevent_destroy`.
- **ECR**, one repository per entry in `ecr_repositories` (the values match the
  image names in `deploy/k8s/*.yaml`). Scan on push. A lifecycle policy keeps the
  `ecr_keep_tagged_images` most recent tagged images and expires untagged images
  after `ecr_untagged_expiry_days`. Tags stay mutable, since the manifests use
  `:latest` today; sub-project 4 may move to commit-SHA tags. Repositories are not
  force-deleted, so a destroy with images present fails rather than losing them.

### Claude API key

- An `aws_secretsmanager_secret` named `anthropic_secret_name`, with a
  `secret_recovery_window_days` recovery window and `prevent_destroy`. There is
  **no `aws_secretsmanager_secret_version`**, so Terraform state never holds the
  value.
- The owner sets the value once, without it reaching shell history:

  ```sh
  read -rs KEY && aws secretsmanager put-secret-value \
    --secret-id ka/anthropic-api-key --secret-string "$KEY"; unset KEY
  ```

- chat-api reads it at startup with its pod role (sub-project 1). No Kubernetes
  Secret ever holds it.

### Network

- VPC `<name_prefix>-vpc` on `vpc_cidr`, with DNS hostnames and DNS resolution
  enabled (both required by EKS and by VPC OpenSearch domains).
- `subnet_count` public subnets, each `cidrsubnet(vpc_cidr, subnet_newbits, i)`, in
  the first availability zones of `aws_region` whose zone ID is not in
  `excluded_az_ids`. Public IPs are assigned on launch, and the subnets are tagged
  `kubernetes.io/role/elb = 1` so the load balancer controller can discover them.
- An internet gateway and one public route table (`0.0.0.0/0` to the gateway). No
  NAT gateway and no VPC endpoints.
- Security group `<name_prefix>-opensearch`: ingress TCP 443 from
  `opensearch_ingress_cidrs` only.

### Budget

`<name_prefix>-monthly`: a COST budget, MONTHLY, `budget_limit_usd`, with no
filters. It emails every address in `alert_emails` at each percentage in
`budget_actual_thresholds_pct` (actual spend) and `budget_forecast_thresholds_pct`
(forecast spend).

### IAM

Role names are built from `name_prefix`.

**EKS roles**

| Role | Trusted by | Managed policies |
|---|---|---|
| `<prefix>-eks-cluster` | `eks.amazonaws.com` | `AmazonEKSClusterPolicy` |
| `<prefix>-eks-node` | `ec2.amazonaws.com` | `AmazonEKSWorkerNodePolicy`, `AmazonEKS_CNI_Policy`, `AmazonEC2ContainerRegistryReadOnly` |

**Pod roles** trust `pods.eks.amazonaws.com` for `sts:AssumeRole` and
`sts:TagSession`:

| Role | Permissions |
|---|---|
| `<prefix>-chat-api` | `bedrock:InvokeModel` and `bedrock:InvokeModelWithResponseStream` on the foundation models in `chat_api_bedrock_model_ids`; `secretsmanager:GetSecretValue` on the Claude key secret |
| `<prefix>-ingestion` | `s3:ListBucket` on the docs bucket and `s3:GetObject` on its objects; `bedrock:InvokeModel` on the models in `ingestion_bedrock_model_ids` |
| `<prefix>-aws-lb-controller` | The AWS Load Balancer Controller's published IAM policy, read from `lb_controller_policy_file` and vendored at the controller version sub-project 4 installs. The two change together. |

No pod role has OpenSearch permissions; the domain is reached only from inside
the VPC.

**Service-linked role:** `aws_iam_service_linked_role` for
`opensearchservice.amazonaws.com`. It is missing today, and a VPC domain cannot be
created without it. The EKS, EKS node group, Elastic Load Balancing and Auto
Scaling service-linked roles already exist and are not managed here.

### Outputs

Read by the daily stack through `terraform_remote_state`:

| Output | Value |
|---|---|
| `aws_region` | the region |
| `vpc_id`, `subnet_ids` | the network |
| `opensearch_security_group_id` | the OpenSearch security group |
| `eks_cluster_role_arn`, `eks_node_role_arn` | EKS roles |
| `pod_role_arns` | map: `chat-api`, `ingestion`, `aws-lb-controller` |
| `admin_principal_arn` | ARN of the identity that applies the foundation; the daily stack grants it cluster admin |
| `docs_bucket` | the docs bucket name |
| `ecr_repository_urls` | map of repository name to URL |
| `anthropic_secret_arn` | the Claude key secret |
| `anthropic_secret_name` | the secret's name, for `put-secret-value` |
| `verify_expectations` | budget name, limit and notification count, the `Project` tag, and the model IDs — what `verify.sh` checks the account against |

## Running it

Once, on the owner's laptop:

```sh
brew uninstall terraform && brew install tfenv
tfenv install 1.5.7 && tfenv use 1.5.7     # global default, for other projects
tfenv install                              # reads .terraform-version: 1.16.2

cd terraform/bootstrap
cp private.auto.tfvars.example private.auto.tfvars   # fill in
terraform init && terraform apply

cd ../foundation
cp private.auto.tfvars.example private.auto.tfvars   # fill in
cp backend.hcl.example backend.hcl                   # bucket from bootstrap output
terraform init -backend-config=backend.hcl
terraform plan && terraform apply

read -rs KEY && aws secretsmanager put-secret-value \
  --secret-id ka/anthropic-api-key --secret-string "$KEY"; unset KEY
./verify.sh
```

Daily (sub-project 4): apply `terraform/daily` in the morning and destroy it in
the evening, wrapped with the image build and push and the Kubernetes deploy. The
foundation is never destroyed as part of the daily cycle.

## Cost

| Item | $/month |
|---|---|
| Secrets Manager secret | 0.40 |
| S3, state and docs (under 1 GB) | < 0.05 |
| ECR, a few images | ~0.10 |
| VPC, subnets, route table, internet gateway, security group, IAM, budget without actions | 0 |
| **Total** | **~0.50** |

## Verification

1. `terraform fmt -check` and `terraform validate` pass in both stacks.
2. No literal configuration in code: a check that searches `terraform/*/*.tf` for
   quoted literals of the configured values — the region, zone IDs, the VPC CIDR,
   `ka-` names, model IDs and the secret name — finds none. It matches quoted
   strings only, so variable names such as `anthropic_secret_name` do not trip it.
3. The owner reviews `terraform plan` before every apply. The first foundation plan
   shows only creates, and a second plan right after apply shows no changes.
4. Remote state: `terraform init -backend-config=backend.hcl` succeeds from a clean
   checkout; a `.tflock` object appears in the bucket during a plan and is gone
   afterwards.
5. IAM, checked with `aws iam simulate-principal-policy`, needing no deploy.
   `terraform/foundation/verify.sh` holds these checks so they can be re-run after
   any IAM change:
   - `<prefix>-chat-api`: `bedrock:InvokeModel` allowed on Titan v2 and
     gpt-oss-20b and denied on an Anthropic model;
     `secretsmanager:GetSecretValue` allowed on its secret; `s3:GetObject` denied
     on the docs bucket.
   - `<prefix>-ingestion`: `s3:GetObject` allowed on the docs bucket;
     `bedrock:InvokeModel` denied on gpt-oss-20b;
     `secretsmanager:GetSecretValue` denied.
6. After the owner sets the key, `aws secretsmanager describe-secret` shows a
   current version. The value is never printed.
7. `aws budgets describe-budget` shows $50 with four notifications, and
   `aws resourcegroupstaggingapi get-resources --tag-filters
   Key=Project,Values=knowledge-assistant` lists the foundation's resources.

## Consequences

- A new role for the daily stack means editing the foundation and applying it;
  the daily stack never creates IAM.
- The owner's machine changes: Homebrew's `terraform` is replaced by tfenv.
  Projects without a `.terraform-version` still get 1.5.7.
- `terraform destroy` on the foundation fails on the state bucket, the docs bucket
  and the secret, by design. Removing the foundation is a manual procedure
  documented in the README's Deploy section, which is rewritten for these steps
  (the section today describes a Helm chart that does not exist).
- CLAUDE.md gains one line: Terraform lives under `terraform/`, all configuration
  is in tfvars, and Claude never runs `terraform apply` or `destroy` unless asked.
- Sub-project 1 must make chat-api read the Claude key secret at startup, which
  brings in the AWS SDK that the Bedrock clients also need.
- Sub-project 4 inherits: a local safety net for a forgotten evening teardown
  (for example a macOS `launchd` job, which runs a missed schedule when the laptop
  wakes); the Pod Identity agent add-on and associations; the load balancer
  controller version pinned together with its vendored policy; and images built on
  Apple Silicon being arm64, which points to Graviton nodes (cheaper than the
  x86 equivalent) or a cross-build to amd64.
- `deploy/k8s/*.yaml` still carries IRSA comments and `KA_LLM_MODE: bedrock`;
  sub-project 4 rewrites them.
- The account's existing $5 budget will alert constantly once the daily
  environment runs; the owner may retire it.
