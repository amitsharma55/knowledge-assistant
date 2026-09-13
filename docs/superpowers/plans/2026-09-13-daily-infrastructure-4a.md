# Daily Infrastructure (Sub-project 4a) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a disposable `terraform/daily/` stack that stands up an EKS cluster, one managed node group, and a single-node OpenSearch domain — all wired to the foundation stack's outputs — proven offline with `terraform test` before the owner ever applies it.

**Architecture:** One new Terraform root stack under `terraform/daily/`, AWS-provider-only. It reads the foundation stack through `terraform_remote_state` and creates only compute: the cluster and node group via the community `terraform-aws-modules/eks` module (reusing foundation's IAM roles), a hand-rolled `aws_opensearch_domain`, and three `aws_eks_pod_identity_association` resources. It creates no IAM, no Kubernetes workloads, and no Helm releases. All logic is unit-tested with `terraform test` against a mocked AWS provider, so no task needs AWS credentials; a `verify.sh` checks the live account after the owner applies.

**Tech Stack:** Terraform 1.16.2 (via tfenv, pinned in `.terraform-version`), `hashicorp/aws ~> 6.64`, `terraform-aws-modules/eks ~> 21.25`, `terraform test` with `mock_provider`, bash + jq, AWS CLI v2.

**Spec:** `docs/superpowers/specs/2026-09-13-daily-infrastructure-4a-design.md`

## Global Constraints

- Terraform `~> 1.16` (repo pins `1.16.2` in `.terraform-version`). Provider `hashicorp/aws ~> 6.64`. EKS module pinned `~> 21.25` (requires `aws >= 6.59`; also pulls `hashicorp/tls` and `hashicorp/time`, which Terraform installs transitively — the root stack declares only `aws`).
- Every tunable value is a `variable` with a `type`, a `description`, and **no `default`**. Values live in the stack's committed `terraform.tfvars` or its gitignored `private.auto.tfvars`.
- Literals are allowed in `.tf` only for AWS-defined constants (managed-policy names, service enum values, the TLS security-policy name), the security baseline (encryption on, `enforce_https`), and fixed name suffixes appended to `var.name_prefix` (e.g. `-daily`). Partition and account come from `data.aws_partition` / `data.aws_caller_identity`, never as literals. `terraform/check-literals.sh` fails if any quoted tfvars value appears in a `.tf` file.
- The repository is **public**: no account ID, bucket name containing an account ID, or email in any committed file. Anything carrying the account ID (the foundation state bucket, `allowed_account_ids`) lives in gitignored `private.auto.tfvars`; a committed `private.auto.tfvars.example` documents it with fixture values (`123456789012`). Tests use fixtures only.
- **Claude never runs `terraform apply`, `destroy`, `import`, or `init` against the real S3 backend.** Offline work uses `terraform init -backend=false`. `verify.sh` is owner-only.
- **Do not commit.** The owner commits when they choose; each task ends at a checkpoint where `make tf-check` passes.
- Shell scripts must run on macOS bash 3.2: no `mapfile`, no associative arrays, no `sed -i`, and no loops whose last command is a bare `&&` list (use `if`).
- After every task, `make tf-check` passes. `terraform/check.sh` auto-discovers any directory under `terraform/` containing a `versions.tf`, so the new stack is picked up with no Makefile change.

## File Structure

| Path | Responsibility |
|---|---|
| `terraform/daily/versions.tf` | Terraform + AWS provider constraints; the `aws` provider block (region, allowed accounts, default tags) |
| `terraform/daily/backend.tf` | Empty `s3` backend block (config supplied at init via `backend.hcl`) |
| `terraform/daily/backend.hcl.example` | Documented fallback backend config (fixture values) |
| `terraform/daily/data.tf` | `terraform_remote_state.foundation`, `aws_caller_identity`, `aws_partition`; a `locals` block exposing foundation outputs |
| `terraform/daily/variables.tf` | Every variable for the stack |
| `terraform/daily/eks.tf` | The EKS module invocation: cluster, node group, access entry, core add-ons |
| `terraform/daily/opensearch.tf` | The single-node `aws_opensearch_domain` and its access policy |
| `terraform/daily/pod-identity.tf` | Three `aws_eks_pod_identity_association` resources |
| `terraform/daily/outputs.tf` | The 4a → 4b contract plus `verify_expectations` |
| `terraform/daily/terraform.tfvars` | Committed configuration (no account-identifying values) |
| `terraform/daily/private.auto.tfvars.example` | Documents the gitignored private values (foundation state bucket, allowed accounts) |
| `terraform/daily/tests/daily.tftest.hcl` | Unit tests: mocked AWS provider + remote-state override |
| `terraform/daily/verify.sh` | Owner-run post-apply checks against the live account |
| `terraform/daily/tests/verify_test.sh` | Test for `verify.sh` using a stand-in AWS CLI |
| `terraform/daily/tests/fake-aws/aws` | Stand-in AWS CLI for `verify_test.sh` |
| `README.md` | `## Deploy` section: add the daily stack's apply/destroy step |
| `CLAUDE.md` | Note the daily stack under the Terraform gotchas |

---

### Task 1: Stack skeleton and offline test harness

Stand up the stack's scaffolding and prove that `terraform test` runs fully offline with the foundation remote-state stubbed. This de-risks the whole plan: if the remote-state override does not work offline, we learn it here before writing any resources.

**Files:**
- Create: `terraform/daily/versions.tf`
- Create: `terraform/daily/backend.tf`
- Create: `terraform/daily/backend.hcl.example`
- Create: `terraform/daily/data.tf`
- Create: `terraform/daily/variables.tf`
- Create: `terraform/daily/terraform.tfvars`
- Create: `terraform/daily/private.auto.tfvars.example`
- Test: `terraform/daily/tests/daily.tftest.hcl`

**Interfaces:**
- Consumes: foundation outputs via `terraform_remote_state` — `vpc_id` (string), `subnet_ids` (list(string)), `opensearch_security_group_id` (string), `eks_cluster_role_arn` (string), `eks_node_role_arn` (string), `pod_role_arns` (map(string) with keys `chat-api`, `ingestion`, `aws-lb-controller`), `admin_principal_arn` (string), `aws_region` (string), `docs_bucket` (string).
- Produces: `local.foundation` (object of the above), used by every later task. `var.name_prefix`, `var.aws_region`, `var.allowed_account_ids`, `var.tags`, `var.foundation_state_bucket`, `var.foundation_state_key` for later tasks.

- [ ] **Step 1: Provider and backend files**

Create `terraform/daily/versions.tf`:

```hcl
terraform {
  required_version = "~> 1.16"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.64"
    }
  }
}

provider "aws" {
  region              = var.aws_region
  allowed_account_ids = var.allowed_account_ids

  default_tags {
    tags = var.tags
  }
}
```

Create `terraform/daily/backend.tf`:

```hcl
# Backend blocks cannot read variables, so every value comes from a gitignored
# backend.hcl: terraform init -backend-config=backend.hcl
terraform {
  backend "s3" {}
}
```

Create `terraform/daily/backend.hcl.example`:

```hcl
# Copy to backend.hcl (gitignored). Set bucket to the bootstrap stack's
# state_bucket_name output, then: terraform init -backend-config=backend.hcl
bucket       = "ka-tfstate-000000000000"
key          = "daily/terraform.tfstate"
region       = "us-east-1"
use_lockfile = true
encrypt      = true
```

- [ ] **Step 2: Data sources and the foundation locals**

Create `terraform/daily/data.tf`:

```hcl
data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

# The foundation stack's outputs. Its state key is fixed; the bucket carries the
# account ID, so it comes from the gitignored private.auto.tfvars.
data "terraform_remote_state" "foundation" {
  backend = "s3"

  config = {
    bucket = var.foundation_state_bucket
    key    = var.foundation_state_key
    region = var.aws_region
  }
}

locals {
  foundation = data.terraform_remote_state.foundation.outputs
}
```

- [ ] **Step 3: Variables and tfvars**

Create `terraform/daily/variables.tf` with the common variables (later tasks append their own):

```hcl
variable "aws_region" {
  description = "Region for the daily stack. Must match the foundation stack."
  type        = string
}

variable "allowed_account_ids" {
  description = "Account IDs Terraform is allowed to act on, as a guard against a wrong profile."
  type        = list(string)
}

variable "name_prefix" {
  description = "Prefix for every named resource. Matches the foundation stack."
  type        = string
}

variable "tags" {
  description = "Default tags applied to every resource."
  type        = map(string)
}

variable "foundation_state_bucket" {
  description = "S3 bucket holding the foundation stack's state (carries the account ID; set in private.auto.tfvars)."
  type        = string
}

variable "foundation_state_key" {
  description = "State key of the foundation stack within the state bucket."
  type        = string
}
```

Create `terraform/daily/terraform.tfvars` (committed; no account-identifying values):

```hcl
aws_region           = "us-east-1"
name_prefix          = "ka"
foundation_state_key = "foundation/terraform.tfstate"

tags = {
  Project   = "knowledge-assistant"
  ManagedBy = "terraform"
  Stack     = "daily"
}
```

Create `terraform/daily/private.auto.tfvars.example` (committed; documents the gitignored file with fixtures):

```hcl
# Copy to private.auto.tfvars (gitignored). These carry the account ID.
allowed_account_ids     = ["123456789012"]
foundation_state_bucket = "ka-tfstate-123456789012"
```

- [ ] **Step 4: Write the failing offline test**

Create `terraform/daily/tests/daily.tftest.hcl`. The `override_data` block replaces the foundation remote-state read so the test needs no S3 and no credentials. Assert the `local.foundation` wiring resolves.

```hcl
# Unit tests for the daily stack, planned against a mocked AWS provider and a
# stubbed foundation remote state: no credentials, no account, no S3.
# Later tasks add resources and run blocks here.

mock_provider "aws" {
  override_during = plan

  override_data {
    target = data.aws_caller_identity.current
    values = {
      account_id = "123456789012"
      arn        = "arn:aws:iam::123456789012:user/tester"
    }
  }

  override_data {
    target = data.aws_partition.current
    values = { partition = "aws" }
  }
}

# The foundation remote state, stubbed with fixture outputs. This is the whole
# 4a -> foundation contract; every field a later task reads must appear here.
override_data {
  target = data.terraform_remote_state.foundation
  values = {
    outputs = {
      aws_region                   = "us-east-1"
      vpc_id                       = "vpc-0abc"
      subnet_ids                   = ["subnet-0a", "subnet-0b"]
      opensearch_security_group_id = "sg-0os"
      eks_cluster_role_arn         = "arn:aws:iam::123456789012:role/zz-eks-cluster"
      eks_node_role_arn            = "arn:aws:iam::123456789012:role/zz-eks-node"
      admin_principal_arn          = "arn:aws:iam::123456789012:user/tester"
      docs_bucket                  = "zz-docs-123456789012"
      pod_role_arns = {
        "chat-api"          = "arn:aws:iam::123456789012:role/zz-chat-api"
        "ingestion"         = "arn:aws:iam::123456789012:role/zz-ingestion"
        "aws-lb-controller" = "arn:aws:iam::123456789012:role/zz-lb"
      }
    }
  }
}

variables {
  aws_region              = "us-east-1"
  allowed_account_ids     = ["123456789012"]
  name_prefix             = "zz"
  foundation_state_bucket = "zz-tfstate-123456789012"
  foundation_state_key    = "foundation/terraform.tfstate"
  tags                    = { Project = "p", ManagedBy = "m", Stack = "daily" }
}

run "foundation_wiring_resolves" {
  command = plan

  assert {
    condition     = local.foundation.vpc_id == "vpc-0abc"
    error_message = "foundation remote-state override did not resolve"
  }

  assert {
    condition     = local.foundation.pod_role_arns["chat-api"] == "arn:aws:iam::123456789012:role/zz-chat-api"
    error_message = "pod_role_arns not readable from foundation outputs"
  }
}
```

- [ ] **Step 5: Run the test — expect it to run offline and pass**

Run: `cd terraform/daily && terraform init -backend=false && terraform test`
Expected: `run "foundation_wiring_resolves"` passes. If Terraform tries to reach S3 for the remote state despite the override, stop — the override syntax or version is wrong; do not add credentials. The fallback (only if the override cannot stub the read) is to feed the foundation values through a variable instead of `terraform_remote_state` in tests; record the change before proceeding.

- [ ] **Step 6: Checkpoint**

Run: `make tf-check`
Expected: PASS (fmt, validate, `terraform test`, script tests, `check-literals.sh` all green). Leave the changes uncommitted for the owner.

---

### Task 2: EKS cluster, node group, access entry, and core add-ons

**Files:**
- Create: `terraform/daily/eks.tf`
- Modify: `terraform/daily/variables.tf` (append)
- Modify: `terraform/daily/terraform.tfvars` (append)
- Test: `terraform/daily/tests/daily.tftest.hcl` (append a run block)

**Interfaces:**
- Consumes: `local.foundation` (`vpc_id`, `subnet_ids`, `eks_cluster_role_arn`, `eks_node_role_arn`, `admin_principal_arn`), `data.aws_partition.current.partition`.
- Produces: `module.eks` with `.cluster_name`, `.cluster_endpoint`, `.cluster_certificate_authority_data`, `.cluster_version`. `local.cluster_name = module.eks.cluster_name` for later tasks. `var.kubernetes_version`, `var.node_instance_types`, `var.node_min_size`, `var.node_desired_size`, `var.node_max_size`.

- [ ] **Step 1: Append EKS variables**

Append to `terraform/daily/variables.tf`:

```hcl
variable "kubernetes_version" {
  description = "EKS control-plane Kubernetes version."
  type        = string
}

variable "node_instance_types" {
  description = "Instance types for the managed node group."
  type        = list(string)
}

variable "node_min_size" {
  description = "Minimum nodes in the managed node group."
  type        = number
}

variable "node_desired_size" {
  description = "Desired nodes in the managed node group."
  type        = number
}

variable "node_max_size" {
  description = "Maximum nodes in the managed node group."
  type        = number
}
```

Append to `terraform/daily/terraform.tfvars`:

```hcl
kubernetes_version  = "1.31"
node_instance_types = ["t3.medium"]
node_min_size       = 1
node_desired_size   = 1
node_max_size       = 2
```

- [ ] **Step 2: Write the failing test for the EKS wiring**

Append to `terraform/daily/tests/daily.tftest.hcl`:

```hcl
run "eks_wiring" {
  command = plan

  # Cluster reuses the foundation cluster role, not a module-created one.
  assert {
    condition     = module.eks.iam_role_arn == "arn:aws:iam::123456789012:role/zz-eks-cluster"
    error_message = "cluster must reuse the foundation eks_cluster_role_arn"
  }

  # Access-entry admin is the foundation admin principal.
  assert {
    condition     = aws_eks_access_entry.admin.principal_arn == "arn:aws:iam::123456789012:user/tester"
    error_message = "admin access entry must use the foundation admin_principal_arn"
  }

  # Cluster name derives from the prefix.
  assert {
    condition     = module.eks.cluster_name == "zz-daily"
    error_message = "cluster name must be <name_prefix>-daily"
  }
}
```

Note: the module exposes the cluster's IAM role ARN as `module.eks.iam_role_arn`. If a `terraform test` failure reports that output name does not exist for the pinned version, run `terraform output` against the module docs for v21.25 and use the correct name (e.g. `cluster_iam_role_arn`); adjust the assertion. The access entry is declared as our own resource (Step 3), not inside the module, so it is assertable directly.

- [ ] **Step 3: Run the test to confirm it fails**

Run: `cd terraform/daily && terraform test -filter=tests/daily.tftest.hcl`
Expected: FAIL — `module.eks` and `aws_eks_access_entry.admin` are not yet defined.

- [ ] **Step 4: Write `eks.tf`**

Create `terraform/daily/eks.tf`. The access entry is declared as a standalone resource (not via the module's `access_entries`) so tests can assert on it directly and so `enable_cluster_creator_admin_permissions` can stay `false`, avoiding a duplicate-principal error when the applying identity equals the admin principal.

```hcl
module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "~> 21.25"

  name               = "${var.name_prefix}-daily"
  kubernetes_version = var.kubernetes_version

  vpc_id     = local.foundation.vpc_id
  subnet_ids = local.foundation.subnet_ids

  # kubectl from the owner's laptop; private access for in-VPC pods.
  endpoint_public_access  = true
  endpoint_private_access = true

  # Access entries only; the admin entry is declared separately below.
  authentication_mode                      = "API"
  enable_cluster_creator_admin_permissions = false

  # Foundation owns identity. Reuse its cluster role; create none here.
  create_iam_role = false
  iam_role_arn    = local.foundation.eks_cluster_role_arn

  # Managed EKS add-ons. eks-pod-identity-agent is required for the Pod
  # Identity associations in pod-identity.tf to function.
  addons = {
    vpc-cni                = {}
    coredns                = {}
    kube-proxy             = {}
    eks-pod-identity-agent = {}
  }

  eks_managed_node_groups = {
    default = {
      instance_types = var.node_instance_types
      capacity_type  = "ON_DEMAND"
      min_size       = var.node_min_size
      desired_size   = var.node_desired_size
      max_size       = var.node_max_size
      subnet_ids     = local.foundation.subnet_ids

      # Reuse the foundation node role; create none here.
      create_iam_role = false
      iam_role_arn    = local.foundation.eks_node_role_arn
    }
  }

  tags = var.tags
}

# The identity that applied the stack gets cluster admin immediately, so the
# owner can update-kubeconfig and run the 4b deploy. Principal comes from
# foundation, which recorded who applied it.
resource "aws_eks_access_entry" "admin" {
  cluster_name  = module.eks.cluster_name
  principal_arn = local.foundation.admin_principal_arn
  type          = "STANDARD"
}

resource "aws_eks_access_policy_association" "admin" {
  cluster_name  = module.eks.cluster_name
  principal_arn = local.foundation.admin_principal_arn
  policy_arn    = "arn:${data.aws_partition.current.partition}:iam::aws:policy/AmazonEKSClusterAdminPolicy"

  access_scope {
    type = "cluster"
  }

  depends_on = [aws_eks_access_entry.admin]
}

locals {
  cluster_name = module.eks.cluster_name
}
```

- [ ] **Step 5: Run the test to confirm it passes**

Run: `cd terraform/daily && terraform init -backend=false && terraform test -filter=tests/daily.tftest.hcl`
Expected: `run "eks_wiring"` passes. If `terraform test` errors that the `tls` or `time` provider tried to act (the module declares them), add `mock_provider "tls" {}` and `mock_provider "time" {}` blocks at the top of the test file and re-run.

- [ ] **Step 6: Checkpoint**

Run: `make tf-check`
Expected: PASS. `check-literals.sh` must stay green — confirm no tfvars value (e.g. `t3.medium`, `1.31`) appears literally in any `.tf`. Leave uncommitted.

---

### Task 3: OpenSearch domain

**Files:**
- Create: `terraform/daily/opensearch.tf`
- Modify: `terraform/daily/variables.tf` (append)
- Modify: `terraform/daily/terraform.tfvars` (append)
- Test: `terraform/daily/tests/daily.tftest.hcl` (append a run block)

**Interfaces:**
- Consumes: `local.foundation` (`subnet_ids`, `opensearch_security_group_id`, `pod_role_arns`), `data.aws_partition.current.partition`, `data.aws_caller_identity.current.account_id`.
- Produces: `aws_opensearch_domain.main` with `.endpoint`. `var.opensearch_engine_version`, `var.opensearch_instance_type`, `var.opensearch_volume_size`, `var.opensearch_tls_policy`.

- [ ] **Step 1: Append OpenSearch variables**

Append to `terraform/daily/variables.tf`:

```hcl
variable "opensearch_engine_version" {
  description = "OpenSearch engine version, e.g. OpenSearch_2.13."
  type        = string
}

variable "opensearch_instance_type" {
  description = "Instance type for the single OpenSearch data node."
  type        = string
}

variable "opensearch_volume_size" {
  description = "EBS gp3 volume size in GiB for the OpenSearch node."
  type        = number
}

variable "opensearch_tls_policy" {
  description = "TLS security policy name for the domain endpoint."
  type        = string
}
```

Append to `terraform/daily/terraform.tfvars`:

```hcl
opensearch_engine_version = "OpenSearch_2.13"
opensearch_instance_type  = "t3.small.search"
opensearch_volume_size    = 20
opensearch_tls_policy     = "Policy-Min-TLS-1-2-2019-07"
```

- [ ] **Step 2: Write the failing test**

Append to `terraform/daily/tests/daily.tftest.hcl`:

```hcl
run "opensearch_is_single_node_vpc" {
  command = plan

  assert {
    condition     = aws_opensearch_domain.main.cluster_config[0].instance_count == 1
    error_message = "OpenSearch must be a single data node"
  }

  assert {
    condition     = aws_opensearch_domain.main.cluster_config[0].zone_awareness_enabled == false
    error_message = "OpenSearch must not be multi-AZ"
  }

  assert {
    condition     = aws_opensearch_domain.main.cluster_config[0].dedicated_master_enabled == false
    error_message = "OpenSearch must not have a dedicated master"
  }

  assert {
    condition     = contains(aws_opensearch_domain.main.vpc_options[0].security_group_ids, "sg-0os")
    error_message = "OpenSearch must attach the foundation security group"
  }

  assert {
    condition     = length(aws_opensearch_domain.main.vpc_options[0].subnet_ids) == 1
    error_message = "single-node OpenSearch must sit in exactly one subnet"
  }

  assert {
    condition     = aws_opensearch_domain.main.domain_endpoint_options[0].enforce_https == true
    error_message = "OpenSearch must enforce HTTPS"
  }

  assert {
    condition     = aws_opensearch_domain.main.encrypt_at_rest[0].enabled == true
    error_message = "OpenSearch must encrypt at rest"
  }
}
```

- [ ] **Step 3: Run the test to confirm it fails**

Run: `cd terraform/daily && terraform test -filter=tests/daily.tftest.hcl`
Expected: FAIL — `aws_opensearch_domain.main` is not defined.

- [ ] **Step 4: Write `opensearch.tf`**

Create `terraform/daily/opensearch.tf`. VPC-isolated, single node, index disposable. The access policy allows only the chat-api and ingestion pod roles; network isolation (the foundation SG) is the outer boundary.

```hcl
# Single-node, VPC-internal, disposable. The index is rebuilt from S3 on every
# bring-up (4b), so nothing here is durable. Placed in exactly one subnet
# because a single data node cannot span AZs.
resource "aws_opensearch_domain" "main" {
  domain_name    = "${var.name_prefix}-daily"
  engine_version = var.opensearch_engine_version

  cluster_config {
    instance_type            = var.opensearch_instance_type
    instance_count           = 1
    zone_awareness_enabled   = false
    dedicated_master_enabled = false
  }

  ebs_options {
    ebs_enabled = true
    volume_type = "gp3"
    volume_size = var.opensearch_volume_size
  }

  vpc_options {
    subnet_ids         = [local.foundation.subnet_ids[0]]
    security_group_ids = [local.foundation.opensearch_security_group_id]
  }

  encrypt_at_rest {
    enabled = true
  }

  node_to_node_encryption {
    enabled = true
  }

  domain_endpoint_options {
    enforce_https       = true
    tls_security_policy = var.opensearch_tls_policy
  }

  access_policies = data.aws_iam_policy_document.opensearch.json

  tags = var.tags
}

# Only the pods that use OpenSearch may reach its HTTP API. The SG already
# limits the network; this limits the principals.
data "aws_iam_policy_document" "opensearch" {
  statement {
    effect  = "Allow"
    actions = ["es:ESHttp*"]

    principals {
      type = "AWS"
      identifiers = [
        local.foundation.pod_role_arns["chat-api"],
        local.foundation.pod_role_arns["ingestion"],
      ]
    }

    resources = [
      "arn:${data.aws_partition.current.partition}:es:${var.aws_region}:${data.aws_caller_identity.current.account_id}:domain/${var.name_prefix}-daily/*",
    ]
  }
}
```

- [ ] **Step 5: Run the test to confirm it passes**

Run: `cd terraform/daily && terraform test -filter=tests/daily.tftest.hcl`
Expected: all OpenSearch assertions pass. If an attribute is reported as a set rather than a list and an index `[0]` fails, use `one(...)` around the block reference or `tolist(...)`; adjust and re-run.

- [ ] **Step 6: Checkpoint**

Run: `make tf-check`
Expected: PASS. Leave uncommitted.

---

### Task 4: Pod Identity associations

**Files:**
- Create: `terraform/daily/pod-identity.tf`
- Modify: `terraform/daily/variables.tf` (append)
- Modify: `terraform/daily/terraform.tfvars` (append)
- Test: `terraform/daily/tests/daily.tftest.hcl` (append a run block)

**Interfaces:**
- Consumes: `local.cluster_name`, `local.foundation.pod_role_arns`, `module.eks`.
- Produces: `aws_eks_pod_identity_association.chat_api`, `.ingestion`, `.lb_controller`. `var.chat_api_service_account`, `var.ingestion_service_account`, `var.lb_controller_service_account` (each an object `{ namespace, name }`).

- [ ] **Step 1: Append service-account variables**

Append to `terraform/daily/variables.tf`:

```hcl
variable "chat_api_service_account" {
  description = "Namespace and name of the chat-api Kubernetes service account (created in 4b)."
  type        = object({ namespace = string, name = string })
}

variable "ingestion_service_account" {
  description = "Namespace and name of the ingestion service account (created in 4b)."
  type        = object({ namespace = string, name = string })
}

variable "lb_controller_service_account" {
  description = "Namespace and name of the AWS Load Balancer Controller service account (created in 4b)."
  type        = object({ namespace = string, name = string })
}
```

Append to `terraform/daily/terraform.tfvars`:

```hcl
chat_api_service_account      = { namespace = "default", name = "chat-api" }
ingestion_service_account     = { namespace = "default", name = "ingestion" }
lb_controller_service_account = { namespace = "kube-system", name = "aws-load-balancer-controller" }
```

- [ ] **Step 2: Write the failing test**

Append to `terraform/daily/tests/daily.tftest.hcl` (the `variables` block already lacks these three; add them there too so the run has values):

First, add to the top-level `variables { ... }` block:

```hcl
  chat_api_service_account      = { namespace = "default", name = "chat-api" }
  ingestion_service_account     = { namespace = "default", name = "ingestion" }
  lb_controller_service_account = { namespace = "kube-system", name = "aws-load-balancer-controller" }
```

Then add the run block:

```hcl
run "pod_identity_bindings" {
  command = plan

  assert {
    condition     = aws_eks_pod_identity_association.chat_api.role_arn == "arn:aws:iam::123456789012:role/zz-chat-api"
    error_message = "chat-api association must bind the foundation chat-api role"
  }

  assert {
    condition     = aws_eks_pod_identity_association.chat_api.service_account == "chat-api"
    error_message = "chat-api association must target the chat-api service account"
  }

  assert {
    condition     = aws_eks_pod_identity_association.ingestion.role_arn == "arn:aws:iam::123456789012:role/zz-ingestion"
    error_message = "ingestion association must bind the foundation ingestion role"
  }

  assert {
    condition     = aws_eks_pod_identity_association.lb_controller.namespace == "kube-system"
    error_message = "lb-controller association must target kube-system"
  }

  assert {
    condition     = aws_eks_pod_identity_association.lb_controller.role_arn == "arn:aws:iam::123456789012:role/zz-lb"
    error_message = "lb-controller association must bind the foundation aws-lb-controller role"
  }
}
```

- [ ] **Step 3: Run the test to confirm it fails**

Run: `cd terraform/daily && terraform test -filter=tests/daily.tftest.hcl`
Expected: FAIL — the associations are not yet defined.

- [ ] **Step 4: Write `pod-identity.tf`**

Create `terraform/daily/pod-identity.tf`. Associations bind by name and do not need the service accounts to exist yet (4b creates them). `depends_on = [module.eks]` ensures the cluster and the pod-identity-agent add-on exist first.

```hcl
# Bind each foundation pod role to the Kubernetes service account that assumes
# it. The service accounts themselves are created in 4b; Pod Identity binds by
# name, so these can exist first. The lb-controller binding is created here even
# though 4b installs the controller, so the IAM wiring stays in Terraform.

resource "aws_eks_pod_identity_association" "chat_api" {
  cluster_name    = local.cluster_name
  namespace       = var.chat_api_service_account.namespace
  service_account = var.chat_api_service_account.name
  role_arn        = local.foundation.pod_role_arns["chat-api"]

  depends_on = [module.eks]
}

resource "aws_eks_pod_identity_association" "ingestion" {
  cluster_name    = local.cluster_name
  namespace       = var.ingestion_service_account.namespace
  service_account = var.ingestion_service_account.name
  role_arn        = local.foundation.pod_role_arns["ingestion"]

  depends_on = [module.eks]
}

resource "aws_eks_pod_identity_association" "lb_controller" {
  cluster_name    = local.cluster_name
  namespace       = var.lb_controller_service_account.namespace
  service_account = var.lb_controller_service_account.name
  role_arn        = local.foundation.pod_role_arns["aws-lb-controller"]

  depends_on = [module.eks]
}
```

- [ ] **Step 5: Run the test to confirm it passes**

Run: `cd terraform/daily && terraform test -filter=tests/daily.tftest.hcl`
Expected: `run "pod_identity_bindings"` passes.

- [ ] **Step 6: Checkpoint**

Run: `make tf-check`
Expected: PASS. Leave uncommitted.

---

### Task 5: Outputs, verify.sh, and docs

**Files:**
- Create: `terraform/daily/outputs.tf`
- Create: `terraform/daily/verify.sh`
- Create: `terraform/daily/tests/verify_test.sh`
- Create: `terraform/daily/tests/fake-aws/aws`
- Modify: `README.md` (`## Deploy` section)
- Modify: `CLAUDE.md` (Terraform gotchas)

**Interfaces:**
- Consumes: `module.eks` (`cluster_name`, `cluster_endpoint`, `cluster_certificate_authority_data`, `cluster_version`), `aws_opensearch_domain.main.endpoint`, `var.aws_region`, node group name from `module.eks`.
- Produces: outputs `cluster_name`, `cluster_endpoint`, `cluster_certificate_authority_data`, `cluster_version`, `opensearch_endpoint`, `region`, `node_group_name`, `verify_expectations`. `verify.sh` reading them.

- [ ] **Step 1: Write `outputs.tf`**

Create `terraform/daily/outputs.tf`:

```hcl
# Read by 4b (application deploy) and by verify.sh through `terraform output -json`.

output "cluster_name" {
  description = "EKS cluster name; pass to `aws eks update-kubeconfig`."
  value       = module.eks.cluster_name
}

output "cluster_endpoint" {
  description = "EKS API server endpoint."
  value       = module.eks.cluster_endpoint
}

output "cluster_certificate_authority_data" {
  description = "Base64 cluster CA, for scripted kubeconfig assembly."
  value       = module.eks.cluster_certificate_authority_data
}

output "cluster_version" {
  description = "Kubernetes version 4b can assert compatibility against."
  value       = module.eks.cluster_version
}

output "opensearch_endpoint" {
  description = "VPC endpoint 4b bakes into ka-config for chat-api and ingestion."
  value       = aws_opensearch_domain.main.endpoint
}

output "region" {
  description = "Region of the daily stack, passed through from foundation."
  value       = var.aws_region
}

output "node_group_name" {
  description = "Managed node group name, for teardown and scale checks in 4c."
  value       = keys(module.eks.eks_managed_node_groups)[0]
}

output "verify_expectations" {
  description = "Values verify.sh checks the live account against."
  value = {
    cluster_name      = module.eks.cluster_name
    expected_addons   = ["vpc-cni", "coredns", "kube-proxy", "eks-pod-identity-agent"]
    node_desired_size = var.node_desired_size
    opensearch_domain = "${var.name_prefix}-daily"
  }
}
```

Note: if `terraform test` or `terraform validate` reports `eks_managed_node_groups` is not a valid module output name for v21.25, replace the `node_group_name` value with the documented output for that version (e.g. `module.eks.eks_managed_node_groups["default"].node_group_id` or the module's `managed_node_groups` output). Verify against the pinned module's outputs.

- [ ] **Step 2: Write the failing test for the verify script**

Create `terraform/daily/tests/verify_test.sh`. It runs `verify.sh` with a fake AWS CLI on `PATH` and a canned outputs JSON, asserting it passes when the live account matches and fails when it does not. Follow the foundation pattern (`VERIFY_OUTPUTS_JSON` + a `fake-aws` stub).

```bash
#!/usr/bin/env bash
# Tests verify.sh against a stand-in AWS CLI, no account required.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
export PATH="$here/fake-aws:$PATH"

outputs='{
  "region": {"value": "us-east-1"},
  "verify_expectations": {"value": {
    "cluster_name": "ka-daily",
    "expected_addons": ["vpc-cni", "coredns", "kube-proxy", "eks-pod-identity-agent"],
    "node_desired_size": 1,
    "opensearch_domain": "ka-daily"
  }}
}'

echo "== healthy account passes"
VERIFY_OUTPUTS_JSON=<(printf '%s' "$outputs") \
  FAKE_CLUSTER_STATUS=ACTIVE FAKE_NODES_READY=1 \
  FAKE_ADDONS="vpc-cni coredns kube-proxy eks-pod-identity-agent" \
  FAKE_OS_STATUS=Active \
  bash "$here/../verify.sh"

echo "== missing add-on fails"
if VERIFY_OUTPUTS_JSON=<(printf '%s' "$outputs") \
  FAKE_CLUSTER_STATUS=ACTIVE FAKE_NODES_READY=1 \
  FAKE_ADDONS="vpc-cni coredns kube-proxy" \
  FAKE_OS_STATUS=Active \
  bash "$here/../verify.sh"; then
  echo "FAIL: verify.sh passed despite a missing add-on"
  exit 1
fi

echo "== OpenSearch not active fails"
if VERIFY_OUTPUTS_JSON=<(printf '%s' "$outputs") \
  FAKE_CLUSTER_STATUS=ACTIVE FAKE_NODES_READY=1 \
  FAKE_ADDONS="vpc-cni coredns kube-proxy eks-pod-identity-agent" \
  FAKE_OS_STATUS=Processing \
  bash "$here/../verify.sh"; then
  echo "FAIL: verify.sh passed despite OpenSearch not Active"
  exit 1
fi

echo "ALL PASS"
```

- [ ] **Step 3: Run the test to confirm it fails**

Run: `bash terraform/daily/tests/verify_test.sh`
Expected: FAIL — `verify.sh` and the `fake-aws/aws` stub do not exist yet.

- [ ] **Step 4: Write the fake AWS CLI stub**

Create `terraform/daily/tests/fake-aws/aws` and make it executable (`chmod +x`). It answers only the calls `verify.sh` makes, driven by `FAKE_*` env vars.

```bash
#!/usr/bin/env bash
# A stand-in for the AWS CLI, just big enough for verify.sh.
set -euo pipefail

arg() {
  local flag=$1
  shift
  while [ $# -gt 0 ]; do
    if [ "$1" = "$flag" ]; then
      echo "$2"
      return
    fi
    shift
  done
}

case "${1:-} ${2:-}" in
  "eks describe-cluster")
    echo "${FAKE_CLUSTER_STATUS:-ACTIVE}"
    ;;
  "eks list-addons")
    # One add-on name per line.
    for a in ${FAKE_ADDONS:-}; do echo "$a"; done
    ;;
  "eks list-nodegroups")
    echo "default"
    ;;
  "eks describe-nodegroup")
    # Ready node count.
    echo "${FAKE_NODES_READY:-1}"
    ;;
  "opensearch describe-domain")
    # "Active" when processing is complete.
    echo "${FAKE_OS_STATUS:-Active}"
    ;;
  *)
    echo "fake-aws: unhandled call: $*" >&2
    exit 2
    ;;
esac
```

- [ ] **Step 5: Write `verify.sh`**

Create `terraform/daily/verify.sh`. It reads `terraform output -json` (or `VERIFY_OUTPUTS_JSON` in tests) and checks the live account. Each AWS call uses the exact `case` keys the stub answers, and passes a `--query`/`--output text` that the stub ignores (the real CLI honors it). Keep it bash 3.2-safe.

```bash
#!/usr/bin/env bash
# verify.sh checks the applied daily stack against its spec: the cluster is
# ACTIVE, the expected add-ons are installed, the node group has its desired
# nodes Ready, and the OpenSearch domain is Active. Every name comes from
# `terraform output`, so nothing configured in tfvars is repeated here. Run it
# after `terraform apply`, with the credentials Terraform used.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)

if [ -n "${VERIFY_OUTPUTS_JSON:-}" ]; then
  outputs=$(cat "$VERIFY_OUTPUTS_JSON")
else
  outputs=$(terraform -chdir="$here" output -json)
fi
out() { jq -r "$1" <<<"$outputs"; }

region=$(out '.region.value')
cluster=$(out '.verify_expectations.value.cluster_name')
os_domain=$(out '.verify_expectations.value.opensearch_domain')
want_nodes=$(out '.verify_expectations.value.node_desired_size')

failures=0
pass() { echo "PASS  $1"; }
fail() {
  echo "FAIL  $1"
  failures=$((failures + 1))
}

# Cluster ACTIVE.
status=$(aws eks describe-cluster --name "$cluster" --region "$region" \
  --query 'cluster.status' --output text)
if [ "$status" = ACTIVE ]; then pass "cluster $cluster is ACTIVE"; else fail "cluster $cluster is $status, want ACTIVE"; fi

# Expected add-ons installed.
installed=$(aws eks list-addons --cluster-name "$cluster" --region "$region" \
  --query 'addons' --output text)
for addon in $(out '.verify_expectations.value.expected_addons[]'); do
  if echo "$installed" | grep -qx "$addon"; then
    pass "add-on $addon installed"
  else
    fail "add-on $addon missing"
  fi
done

# Node group has its desired nodes Ready.
ready=$(aws eks describe-nodegroup --cluster-name "$cluster" --nodegroup-name default \
  --region "$region" --query 'nodegroup.health.readyCount' --output text)
if [ "$ready" -ge "$want_nodes" ] 2>/dev/null; then
  pass "node group has $ready/$want_nodes nodes ready"
else
  fail "node group has $ready ready, want $want_nodes"
fi

# OpenSearch Active (processing complete).
os_status=$(aws opensearch describe-domain --domain-name "$os_domain" --region "$region" \
  --query 'DomainStatus.Processing' --output text)
if [ "$os_status" = Active ]; then pass "OpenSearch $os_domain is Active"; else fail "OpenSearch $os_domain is $os_status, want Active"; fi

if [ "$failures" -ne 0 ]; then
  echo "$failures check(s) failed"
  exit 1
fi
echo "all checks passed"
```

Note: the `--query` expressions above are what the real AWS CLI needs; the `fake-aws` stub ignores them and returns the `FAKE_*` value for each `case` key. When the owner runs this for real, confirm `readyCount` is the correct field on `describe-nodegroup` for the installed CLI version; adjust if AWS renamed it.

- [ ] **Step 6: Run the verify test to confirm it passes**

Run: `bash terraform/daily/tests/verify_test.sh`
Expected: `ALL PASS`. Then `chmod +x terraform/daily/verify.sh terraform/daily/tests/fake-aws/aws` if not already.

- [ ] **Step 7: Update README and CLAUDE.md**

In `README.md`, under the deploy/local instructions, add a short daily-stack subsection after the foundation step:

```markdown
### Daily stack (owner, each working session)

The daily stack holds the expensive, disposable compute — the EKS cluster and
the OpenSearch domain. Bring it up at the start of a session and destroy it at
the end; the document corpus lives in S3 (foundation) and the OpenSearch index
is rebuilt from it on each bring-up.

    cd terraform/daily
    cp private.auto.tfvars.example private.auto.tfvars   # set account values
    cp backend.hcl.example backend.hcl                   # set the state bucket
    terraform init -backend-config=backend.hcl
    terraform apply
    ./verify.sh                                          # after apply

Tear down when done:

    terraform destroy
```

In `CLAUDE.md`, add one line under Gotchas:

```markdown
- `terraform/daily/` is the disposable stack (EKS + OpenSearch), applied and destroyed each working session. It reads the foundation stack via `terraform_remote_state` and creates no IAM. Its OpenSearch index is disposable; 4b rebuilds it from S3. Never leave it applied overnight.
```

- [ ] **Step 8: Final checkpoint**

Run: `make tf-check`
Expected: PASS — fmt, validate, `terraform test` (all four run blocks), and the script tests (including `verify_test.sh`, which `check.sh` discovers under `terraform/daily/tests/`) all green; `check-literals.sh` green. Leave everything uncommitted for the owner to review and commit.

---

## Self-Review

**Spec coverage:**
- Stack layout (`terraform/daily/`, file-per-concern, remote state) → Tasks 1, 2, 3, 4, 5.
- EKS cluster, public+private endpoint, reused cluster role, `API` auth, admin access entry, managed node group with reused node role, core add-ons incl. `eks-pod-identity-agent` → Task 2.
- OpenSearch single-node, no multi-AZ/master, VPC mode with foundation SG, one subnet, encryption, `enforce_https`, IAM-scoped access → Task 3.
- Three Pod Identity associations with correct role→SA mapping; lb-controller association present though controller installs in 4b → Task 4.
- No controller install, no Helm/Kubernetes provider (AWS-provider-only) → enforced by never adding those providers; verified by the offline `terraform test`.
- Outputs (the 4a→4b contract) + `verify_expectations` → Task 5.
- Testing: `terraform test` mocked, `check-literals.sh`, `verify.sh` with a fake CLI, `init -backend=false`, no Go tests → Tasks 1–5.
- Operating model (disposable, reseed from S3 in 4b) → documented in README/CLAUDE (Task 5); no 4a resource depends on it.

**Placeholder scan:** No TBD/TODO. Every code step carries real HCL/bash. The three "if the module output name differs, adjust" notes are genuine version-guards with a concrete default value and a specific check, not placeholders.

**Type consistency:** `local.foundation` shape is fixed in Task 1 and consumed unchanged in Tasks 2–5. `module.eks.cluster_name` / `local.cluster_name` used consistently. Service-account variables are `object({namespace, name})` in Task 4's variables, tfvars, and test block alike. `verify_expectations` keys written in Task 5's output match exactly the keys `verify.sh` and `verify_test.sh` read (`cluster_name`, `expected_addons`, `node_desired_size`, `opensearch_domain`).
