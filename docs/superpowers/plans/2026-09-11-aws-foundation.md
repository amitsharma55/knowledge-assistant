# AWS Permanent Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Terraform `bootstrap` and `foundation` stacks that create every permanent AWS resource the daily environment needs, configured entirely through tfvars and proven offline before the owner applies them.

**Architecture:** Two Terraform root stacks under a top-level `terraform/` folder share one module for hardened S3 buckets. `bootstrap` (local state) creates the Terraform state bucket; `foundation` (S3 backend) creates the network, the documents bucket, ECR, the Claude-key secret, IAM roles for EKS and Pod Identity, and the budget. All logic is unit-tested with `terraform test` against a mocked AWS provider, so no task needs AWS credentials. Only the owner runs `terraform apply`, and then `verify.sh` checks the live account.

**Tech Stack:** Terraform 1.16.2 (via tfenv), `hashicorp/aws` 6.64+, `terraform test` with `mock_provider`, bash + jq, AWS CLI v2.

**Spec:** `docs/superpowers/specs/2026-09-10-aws-foundation-design.md`

## Global Constraints

- Terraform `~> 1.16`; the repository pins `1.16.2` in `.terraform-version`. Provider `hashicorp/aws ~> 6.64`.
- Every tunable value is a `variable` with a `type`, a `description` and **no `default`**. Values live in the stack's committed `terraform.tfvars` or its gitignored `private.auto.tfvars`.
- Literals are allowed in `.tf` files only for: AWS-defined constants (service principals, managed-policy names, IAM actions, API enum values such as `COST`, `ACTUAL`, `GREATER_THAN`, `PERCENTAGE`, `USD`, the `kubernetes.io/role/elb` tag, port `443`, the `0.0.0.0/0` route); the security baseline (`AES256`, public access blocks, the TLS-only policy, versioning `Enabled`, VPC DNS flags); `prevent_destroy`; and fixed name suffixes appended to `var.name_prefix` (such as `-tfstate-`, `-eks-node`). `make tf-check` fails if any quoted tfvars value appears in a `.tf` file.
- Names derive from `var.name_prefix`. Globally unique names append the account ID from `data.aws_caller_identity`.
- The repository is **public**: no account ID or email address in any committed file. Tests use fixture values only (`123456789012`, `zz-*`, `example.test`).
- The region is `us-east-1`, set in tfvars and never in code.
- **Claude never runs `terraform apply`, `terraform destroy`, `terraform import`, or `terraform init` against the real S3 backend.** Offline work uses `terraform init -backend=false`. Task 8 is for the owner only.
- **Do not commit.** The owner commits when they choose; each task ends at a checkpoint.
- Shell scripts must run on macOS's bash 3.2: no `mapfile`, no associative arrays, no `sed -i`, and no loops whose last command is a bare `&&` list (use `if`).
- After every task, `make tf-check` passes.

## File Structure

| Path | Responsibility |
|---|---|
| `.terraform-version` | Pins Terraform 1.16.2 for tfenv inside this repo |
| `.gitignore` | Adds Terraform state, plans, private tfvars, backend config |
| `Makefile` | Adds the `tf-check` target |
| `terraform/check.sh` | Runs every offline check across all stacks and modules |
| `terraform/check-literals.sh` | Fails when a tfvars value also appears in a `.tf` file |
| `terraform/tests/check_literals_test.sh` | Tests for `check-literals.sh` |
| `terraform/modules/private-bucket/{versions,variables,main,outputs}.tf` | A private, encrypted, versioned, TLS-only S3 bucket with `prevent_destroy` |
| `terraform/modules/private-bucket/tests/private_bucket.tftest.hcl` | Module unit tests |
| `terraform/bootstrap/{versions,variables,main,outputs}.tf` | The state bucket |
| `terraform/bootstrap/terraform.tfvars`, `private.auto.tfvars.example` | Bootstrap configuration |
| `terraform/bootstrap/tests/bootstrap.tftest.hcl` | Bootstrap unit tests |
| `terraform/foundation/versions.tf`, `backend.tf`, `data.tf` | Provider, S3 backend, shared data sources |
| `terraform/foundation/variables.tf` | Every foundation variable |
| `terraform/foundation/network.tf` | VPC, subnets, internet gateway, routes, OpenSearch security group |
| `terraform/foundation/storage.tf` | Documents bucket and ECR repositories |
| `terraform/foundation/secrets.tf` | The Claude key secret (no value) |
| `terraform/foundation/iam.tf` | EKS roles, pod roles, OpenSearch service-linked role |
| `terraform/foundation/policies/aws-lb-controller-v3.5.0.json` | Vendored Load Balancer Controller IAM policy |
| `terraform/foundation/budget.tf` | Account-wide monthly budget |
| `terraform/foundation/outputs.tf` | Values the daily stack and `verify.sh` read |
| `terraform/foundation/terraform.tfvars`, `private.auto.tfvars.example`, `backend.hcl.example` | Foundation configuration |
| `terraform/foundation/tests/foundation.tftest.hcl` | Foundation unit tests |
| `terraform/foundation/verify.sh` | Post-apply checks against the live account |
| `terraform/foundation/tests/verify_test.sh`, `tests/fake-aws/aws` | Tests for `verify.sh` with a stand-in AWS CLI |
| `README.md` | `## Deploy` section rewritten |
| `CLAUDE.md` | Terraform commands and rules |

---

### Task 1: Tooling, ignores and the offline check scripts

**Files:**
- Create: `.terraform-version`
- Modify: `.gitignore` (append)
- Modify: `Makefile` (the `.PHONY` line and a new target after `lint`)
- Create: `terraform/check-literals.sh`
- Create: `terraform/check.sh`
- Test: `terraform/tests/check_literals_test.sh`

**Interfaces:**
- Produces: `terraform/check-literals.sh <stack-dir>...` exits 0 when clean, and exits 1 after printing `path/file.tf:LINE:content` for each hit. It reads quoted strings from `<dir>/terraform.tfvars` and `<dir>/*.auto.tfvars`.
- Produces: `terraform/check.sh` (run by `make tf-check`). It checks every directory under `terraform/` or `terraform/modules/` that contains a `versions.tf`, and runs every `*_test.sh` under `terraform/tests/`, `terraform/*/tests/` and `terraform/modules/*/tests/`.

- [ ] **Step 1: Switch the machine to tfenv**

This change to the owner's machine is approved in the spec. Homebrew's `terraform` formula conflicts with tfenv, so it has to be removed first.

```bash
brew uninstall terraform
brew install tfenv
tfenv install 1.5.7
tfenv use 1.5.7
```

- [ ] **Step 2: Pin the repository's Terraform version**

Create `.terraform-version` in the repository root:

```
1.16.2
```

Then, from the repository root:

```bash
tfenv install
terraform version     # expect: Terraform v1.16.2
(cd ~ && terraform version)   # expect: Terraform v1.5.7
```

- [ ] **Step 3: Ignore Terraform's local files**

Append to `.gitignore`:

```
# Terraform
.terraform/
*.tfstate
*.tfstate.*
*.tfplan
private.auto.tfvars
backend.hcl
```

`.terraform.lock.hcl` is **not** ignored; it gets committed.

- [ ] **Step 4: Write the failing test for check-literals.sh**

Create `terraform/tests/check_literals_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for check-literals.sh. Each case builds a throwaway stack directory.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
check="$here/../check-literals.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $*"; exit 1; }

# A value that is in tfvars and also written in code is caught, with file:line.
mkdir -p "$tmp/leaky"
printf 'region = "zz-region-1"\n' >"$tmp/leaky/terraform.tfvars"
printf 'provider "aws" {\n  region = "zz-region-1"\n}\n' >"$tmp/leaky/main.tf"
if out=$("$check" "$tmp/leaky" 2>&1); then fail "leaky stack passed"; fi
grep -q 'main.tf:2:' <<<"$out" || fail "hit not reported as file:line: $out"

# The same value referenced through a variable passes, and a variable whose
# name contains a value's text does not trip the check.
mkdir -p "$tmp/clean"
printf 'zz_region = "zz-region-1"\nzz_secret_name = "zz/secret"\n' >"$tmp/clean/terraform.tfvars"
printf 'variable "zz_region" {\n  type = string\n}\nvariable "zz_secret_name" {\n  type = string\n}\nprovider "aws" {\n  region = var.zz_region\n}\n' >"$tmp/clean/main.tf"
"$check" "$tmp/clean" >/dev/null || fail "clean stack failed"

# Values from private *.auto.tfvars count too.
mkdir -p "$tmp/private"
printf 'x = "a"\n' >"$tmp/private/terraform.tfvars"
printf 'alert = "zz@example.test"\n' >"$tmp/private/private.auto.tfvars"
printf 'locals {\n  e = "zz@example.test"\n}\n' >"$tmp/private/main.tf"
if "$check" "$tmp/private" >/dev/null 2>&1; then fail "private literal passed"; fi

# A directory without tfvars (a module) is skipped.
mkdir -p "$tmp/module"
printf 'locals {\n  a = "anything"\n}\n' >"$tmp/module/main.tf"
"$check" "$tmp/module" >/dev/null || fail "module without tfvars failed"

echo "check-literals: all tests passed"
```

```bash
chmod +x terraform/tests/check_literals_test.sh
```

- [ ] **Step 5: Run it to verify it fails**

Run: `bash terraform/tests/check_literals_test.sh`
Expected: FAIL. The first case fails because `check-literals.sh` does not exist.

- [ ] **Step 6: Implement check-literals.sh**

Create `terraform/check-literals.sh`:

```bash
#!/usr/bin/env bash
# check-literals.sh <stack-dir>... fails when a quoted value from a stack's
# tfvars files also appears in that stack's .tf files. Configuration belongs in
# tfvars; a value written in both places is a value that will one day be
# changed in only one of them. Matching whole quoted strings means a variable
# name such as anthropic_secret_name never matches its value.
#
# A value that happens to equal a quoted label in code (a resource or variable
# name) is reported too. That is a false positive, but a loud one: rename the
# label.
set -euo pipefail

status=0
for dir in "$@"; do
  patterns=$(cat "$dir"/terraform.tfvars "$dir"/*.auto.tfvars 2>/dev/null |
    grep -oE '"[^"]+"' | sort -u || true)
  if [ -z "$patterns" ]; then
    continue
  fi
  for tf in "$dir"/*.tf; do
    if [ ! -f "$tf" ]; then
      continue
    fi
    if grep -nF -f <(printf '%s\n' "$patterns") "$tf" | sed "s|^|$tf:|"; then
      status=1
    fi
  done
done

if [ "$status" -eq 0 ]; then
  echo "No tfvars values found in .tf files."
fi
exit "$status"
```

```bash
chmod +x terraform/check-literals.sh
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `bash terraform/tests/check_literals_test.sh`
Expected: `check-literals: all tests passed`

- [ ] **Step 8: Write check.sh**

Create `terraform/check.sh`:

```bash
#!/usr/bin/env bash
# check.sh runs every offline check on the Terraform code: formatting,
# validation, `terraform test` against a mocked AWS provider, the rule that
# configuration lives in tfvars (check-literals.sh), and the shell script
# tests. It needs no AWS credentials and never touches an account or the S3
# backend. Paths are assumed to contain no spaces.
set -euo pipefail

root=$(cd "$(dirname "$0")" && pwd)

echo "== terraform fmt"
terraform fmt -check -recursive "$root"

dirs=""
for d in "$root"/*/ "$root"/modules/*/; do
  if [ -f "$d/versions.tf" ]; then
    dirs="$dirs ${d%/}"
  fi
done

for d in $dirs; do
  echo "== ${d#"$root"/}"
  terraform -chdir="$d" init -backend=false -input=false >/dev/null
  terraform -chdir="$d" validate
  if compgen -G "$d/tests/*.tftest.hcl" >/dev/null; then
    terraform -chdir="$d" test
  fi
done

echo "== configuration lives in tfvars"
# $dirs is deliberately unquoted: a space-separated list of paths.
"$root/check-literals.sh" $dirs

echo "== script tests"
for t in "$root"/tests/*_test.sh "$root"/*/tests/*_test.sh "$root"/modules/*/tests/*_test.sh; do
  if [ -f "$t" ]; then
    bash "$t"
  fi
done

echo "All Terraform checks passed."
```

```bash
chmod +x terraform/check.sh
```

- [ ] **Step 9: Add the make target**

In `Makefile`, change the first line to:

```make
.PHONY: dev-up dev-down embed-pull reset-index seed run-api run-ingest run-ui test lint tf-check build
```

and add after the `lint` target:

```make
tf-check:
	terraform/check.sh
```

(The recipe line starts with a tab.)

- [ ] **Step 10: Run the checks**

Run: `make tf-check`
Expected: ends with `All Terraform checks passed.` No stacks exist yet, so this proves only the wiring and the literal-check tests.

- [ ] **Step 11: Checkpoint**

Run `git status --short` and confirm the new and modified files are exactly the ones listed for this task. Do not commit.

---

### Task 2: The private-bucket module and the bootstrap stack

**Files:**
- Create: `terraform/modules/private-bucket/versions.tf`, `variables.tf`, `main.tf`, `outputs.tf`
- Test: `terraform/modules/private-bucket/tests/private_bucket.tftest.hcl`
- Create: `terraform/bootstrap/versions.tf`, `variables.tf`, `main.tf`, `outputs.tf`, `terraform.tfvars`, `private.auto.tfvars.example`
- Test: `terraform/bootstrap/tests/bootstrap.tftest.hcl`

**Interfaces:**
- Produces: module `private-bucket` with inputs `name` (string) and `noncurrent_version_days` (number), and outputs `name` (string) and `arn` (string). Its resources are all named `this`.
- Produces: bootstrap output `state_bucket_name` (string), equal to `<name_prefix>-tfstate-<account-id>`.

- [ ] **Step 1: Write the failing module test**

Create `terraform/modules/private-bucket/tests/private_bucket.tftest.hcl`:

```hcl
mock_provider "aws" {
  override_during = plan
}

variables {
  name                    = "zz-test-bucket"
  noncurrent_version_days = 7
}

run "applies_the_security_baseline" {
  command = plan

  assert {
    condition     = aws_s3_bucket.this.bucket == "zz-test-bucket"
    error_message = "The bucket name must come from var.name."
  }

  assert {
    condition     = one(aws_s3_bucket_versioning.this.versioning_configuration).status == "Enabled"
    error_message = "Versioning must be enabled."
  }

  assert {
    condition     = one(one(aws_s3_bucket_lifecycle_configuration.this.rule).noncurrent_version_expiration).noncurrent_days == 7
    error_message = "Noncurrent versions must expire after var.noncurrent_version_days."
  }

  assert {
    condition     = one(one(aws_s3_bucket_server_side_encryption_configuration.this.rule).apply_server_side_encryption_by_default).sse_algorithm == "AES256"
    error_message = "The bucket must use SSE-S3 encryption."
  }

  assert {
    condition = alltrue([
      aws_s3_bucket_public_access_block.this.block_public_acls,
      aws_s3_bucket_public_access_block.this.block_public_policy,
      aws_s3_bucket_public_access_block.this.ignore_public_acls,
      aws_s3_bucket_public_access_block.this.restrict_public_buckets,
    ])
    error_message = "All four public access blocks must be on."
  }

  assert {
    condition = (
      jsondecode(aws_s3_bucket_policy.this.policy).Statement[0].Effect == "Deny" &&
      jsondecode(aws_s3_bucket_policy.this.policy).Statement[0].Condition.Bool["aws:SecureTransport"] == "false"
    )
    error_message = "The bucket policy must deny requests not made over TLS."
  }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `terraform -chdir=terraform/modules/private-bucket init -backend=false && terraform -chdir=terraform/modules/private-bucket test`
Expected: FAIL. `init` or `test` errors because the module has no configuration yet, or the assertions reference undeclared resources.

- [ ] **Step 3: Implement the module**

`terraform/modules/private-bucket/versions.tf`:

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
```

`terraform/modules/private-bucket/variables.tf`:

```hcl
variable "name" {
  description = "Globally unique bucket name."
  type        = string
}

variable "noncurrent_version_days" {
  description = "Days to keep noncurrent object versions before they expire."
  type        = number

  validation {
    condition     = var.noncurrent_version_days >= 1
    error_message = "noncurrent_version_days must be at least 1."
  }
}
```

`terraform/modules/private-bucket/main.tf`:

```hcl
# A private S3 bucket with the security baseline every bucket in this project
# gets: versioned, SSE-S3 encrypted, never public, TLS only, and protected from
# `terraform destroy`. The baseline is fixed here rather than configured; see
# the foundation spec.

resource "aws_s3_bucket" "this" {
  bucket = var.name

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_s3_bucket_versioning" "this" {
  bucket = aws_s3_bucket.this.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "this" {
  bucket = aws_s3_bucket.this.id

  rule {
    id     = "expire-noncurrent-versions"
    status = "Enabled"

    filter {}

    noncurrent_version_expiration {
      noncurrent_days = var.noncurrent_version_days
    }
  }

  # A lifecycle rule on noncurrent versions needs versioning in place first.
  depends_on = [aws_s3_bucket_versioning.this]
}

resource "aws_s3_bucket_server_side_encryption_configuration" "this" {
  bucket = aws_s3_bucket.this.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "this" {
  bucket = aws_s3_bucket.this.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_policy" "this" {
  bucket = aws_s3_bucket.this.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "DenyInsecureTransport"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:*"
      Resource  = [aws_s3_bucket.this.arn, "${aws_s3_bucket.this.arn}/*"]
      Condition = { Bool = { "aws:SecureTransport" = "false" } }
    }]
  })

  # Putting a policy while the public access block is still being applied
  # races and can fail.
  depends_on = [aws_s3_bucket_public_access_block.this]
}
```

`terraform/modules/private-bucket/outputs.tf`:

```hcl
output "name" {
  description = "The bucket name."
  value       = aws_s3_bucket.this.bucket
}

output "arn" {
  description = "The bucket ARN."
  value       = aws_s3_bucket.this.arn
}
```

- [ ] **Step 4: Run the module test to verify it passes**

Run: `terraform -chdir=terraform/modules/private-bucket init -backend=false && terraform -chdir=terraform/modules/private-bucket test`
Expected: `Success! 1 passed, 0 failed.`

- [ ] **Step 5: Write the failing bootstrap test**

Create `terraform/bootstrap/tests/bootstrap.tftest.hcl`:

```hcl
mock_provider "aws" {
  override_during = plan

  override_data {
    target = data.aws_caller_identity.current
    values = {
      account_id = "123456789012"
    }
  }
}

variables {
  aws_region                    = "us-east-1"
  allowed_account_ids           = ["123456789012"]
  name_prefix                   = "zz"
  tags                          = { Project = "p", ManagedBy = "m", Stack = "bootstrap" }
  state_noncurrent_version_days = 30
}

run "names_the_state_bucket_from_prefix_and_account" {
  command = plan

  assert {
    condition     = output.state_bucket_name == "zz-tfstate-123456789012"
    error_message = "The state bucket must be named <name_prefix>-tfstate-<account-id>."
  }
}

run "rejects_a_malformed_account_id" {
  command = plan

  variables {
    allowed_account_ids = ["not-an-account"]
  }

  expect_failures = [var.allowed_account_ids]
}

run "rejects_tags_without_a_stack" {
  command = plan

  variables {
    tags = { Project = "p", ManagedBy = "m" }
  }

  expect_failures = [var.tags]
}
```

Test assertions cannot reach into a child module's resources, so this file checks only what the bootstrap stack itself decides. The module's own test proves the bucket's security baseline and noncurrent-version expiry.

- [ ] **Step 6: Run it to verify it fails**

Run: `terraform -chdir=terraform/bootstrap init -backend=false && terraform -chdir=terraform/bootstrap test`
Expected: FAIL, because the bootstrap configuration does not exist yet.

- [ ] **Step 7: Implement the bootstrap stack**

`terraform/bootstrap/versions.tf`:

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

`terraform/bootstrap/variables.tf`:

```hcl
variable "aws_region" {
  description = "AWS region for every resource in this stack."
  type        = string
}

variable "allowed_account_ids" {
  description = "AWS account IDs Terraform may operate on; any other active credentials are refused. Private: set in private.auto.tfvars."
  type        = list(string)

  validation {
    condition     = length(var.allowed_account_ids) > 0 && alltrue([for id in var.allowed_account_ids : can(regex("^[0-9]{12}$", id))])
    error_message = "allowed_account_ids must list one or more 12-digit AWS account IDs."
  }
}

variable "name_prefix" {
  description = "Prefix for every resource name."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,14}$", var.name_prefix))
    error_message = "name_prefix must be 2-15 lowercase letters, digits or hyphens, starting with a letter."
  }
}

variable "tags" {
  description = "Tags applied to every resource through the provider's default_tags. Must include Project, ManagedBy and Stack."
  type        = map(string)

  validation {
    condition     = alltrue([for k in ["Project", "ManagedBy", "Stack"] : contains(keys(var.tags), k)])
    error_message = "tags must include Project, ManagedBy and Stack."
  }
}

variable "state_noncurrent_version_days" {
  description = "Days to keep noncurrent versions of Terraform state objects."
  type        = number

  validation {
    condition     = var.state_noncurrent_version_days >= 1
    error_message = "state_noncurrent_version_days must be at least 1."
  }
}
```

`terraform/bootstrap/main.tf`:

```hcl
# The one bucket every other stack keeps its state in. This stack's own state
# stays local (and gitignored): losing it is harmless, since the bucket can be
# imported back.

data "aws_caller_identity" "current" {}

module "state_bucket" {
  source = "../modules/private-bucket"

  # The account ID makes the name globally unique without committing it.
  name                    = "${var.name_prefix}-tfstate-${data.aws_caller_identity.current.account_id}"
  noncurrent_version_days = var.state_noncurrent_version_days
}
```

`terraform/bootstrap/outputs.tf`:

```hcl
output "state_bucket_name" {
  description = "Bucket for Terraform state; copy it into foundation/backend.hcl."
  value       = module.state_bucket.name
}
```

`terraform/bootstrap/terraform.tfvars`:

```hcl
aws_region  = "us-east-1"
name_prefix = "ka"

tags = {
  Project   = "knowledge-assistant"
  ManagedBy = "terraform"
  Stack     = "bootstrap"
}

state_noncurrent_version_days = 90
```

`terraform/bootstrap/private.auto.tfvars.example`:

```hcl
# Copy to private.auto.tfvars (gitignored) and fill in. Never commit the copy:
# the repository is public.
#   aws sts get-caller-identity --query Account --output text
allowed_account_ids = ["000000000000"]
```

- [ ] **Step 8: Run the bootstrap test to verify it passes**

Run: `terraform fmt -recursive terraform && terraform -chdir=terraform/bootstrap init -backend=false && terraform -chdir=terraform/bootstrap test`
Expected: `Success! 3 passed, 0 failed.`

- [ ] **Step 9: Run all checks**

Run: `make tf-check`
Expected: `All Terraform checks passed.` The output includes `== modules/private-bucket` and `== bootstrap`.

- [ ] **Step 10: Checkpoint**

Run `git status --short`. The new files are the module, the bootstrap stack, their tests, and both stacks' `.terraform.lock.hcl` (commit-worthy). No `terraform.tfstate` exists. Do not commit.

---

### Task 3: Foundation skeleton and network

**Files:**
- Create: `terraform/foundation/versions.tf`, `backend.tf`, `backend.hcl.example`, `data.tf`, `variables.tf`, `network.tf`, `terraform.tfvars`, `private.auto.tfvars.example`
- Test: `terraform/foundation/tests/foundation.tftest.hcl`

**Interfaces:**
- Produces: `data.aws_caller_identity.current`, `data.aws_partition.current` and `data.aws_availability_zones.available`, which later tasks use.
- Produces: resources `aws_vpc.main`, `aws_subnet.public` (count), `aws_internet_gateway.main`, `aws_route_table.public`, `aws_security_group.opensearch`, and `aws_vpc_security_group_ingress_rule.opensearch_https` (for_each over the CIDRs).
- Produces: `local.subnet_azs` (list of zone names).

- [ ] **Step 1: Write the failing foundation test**

Create `terraform/foundation/tests/foundation.tftest.hcl`:

```hcl
# Unit tests for the foundation stack, planned against a mocked AWS provider:
# no credentials, no account. Later tasks add variables and run blocks here.

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
    values = {
      partition = "aws"
    }
  }

  override_data {
    target = data.aws_availability_zones.available
    values = {
      names    = ["zz-1a", "zz-1b", "zz-1c", "zz-1d"]
      zone_ids = ["zz1-az1", "zz1-az3", "zz1-az4", "zz1-az6"]
    }
  }
}

variables {
  # common
  aws_region          = "us-east-1"
  allowed_account_ids = ["123456789012"]
  name_prefix         = "zz"
  tags                = { Project = "p", ManagedBy = "m", Stack = "foundation" }

  # network
  vpc_cidr                 = "10.99.0.0/16"
  subnet_count             = 2
  subnet_newbits           = 4
  excluded_az_ids          = ["zz1-az3"]
  opensearch_ingress_cidrs = ["10.99.0.0/16"]
}

run "network_skips_excluded_zones" {
  command = plan

  assert {
    condition     = [for s in aws_subnet.public : s.availability_zone] == ["zz-1a", "zz-1c"]
    error_message = "Subnets must use the first zones whose IDs are not in excluded_az_ids."
  }

  assert {
    condition     = [for s in aws_subnet.public : s.cidr_block] == ["10.99.0.0/20", "10.99.16.0/20"]
    error_message = "Subnet CIDRs must be cidrsubnet(vpc_cidr, subnet_newbits, i)."
  }

  assert {
    condition     = alltrue([for s in aws_subnet.public : s.tags["kubernetes.io/role/elb"] == "1" && s.map_public_ip_on_launch])
    error_message = "Public subnets must be tagged for the load balancer controller and assign public IPs."
  }

  assert {
    condition     = aws_vpc.main.enable_dns_hostnames && aws_vpc.main.enable_dns_support
    error_message = "EKS and VPC OpenSearch domains need DNS support and DNS hostnames."
  }

  assert {
    condition     = one(aws_route_table.public.route).cidr_block == "0.0.0.0/0"
    error_message = "The public route table must send 0.0.0.0/0 to the internet gateway."
  }
}

run "opensearch_accepts_https_from_configured_cidrs_only" {
  command = plan

  assert {
    condition     = keys(aws_vpc_security_group_ingress_rule.opensearch_https) == ["10.99.0.0/16"]
    error_message = "OpenSearch ingress must come from opensearch_ingress_cidrs only."
  }

  assert {
    condition = alltrue([
      for r in aws_vpc_security_group_ingress_rule.opensearch_https :
      r.from_port == 443 && r.to_port == 443 && r.ip_protocol == "tcp"
    ])
    error_message = "OpenSearch ingress must be TCP 443."
  }
}

run "rejects_too_few_usable_zones" {
  command = plan

  variables {
    subnet_count = 4
  }

  expect_failures = [aws_vpc.main]
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `terraform -chdir=terraform/foundation init -backend=false && terraform -chdir=terraform/foundation test`
Expected: FAIL, because the foundation configuration does not exist yet.

- [ ] **Step 3: Implement the skeleton**

`terraform/foundation/versions.tf`:

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

`terraform/foundation/backend.tf`:

```hcl
# Backend blocks cannot read variables, so every value comes from a gitignored
# backend.hcl: terraform init -backend-config=backend.hcl
terraform {
  backend "s3" {}
}
```

`terraform/foundation/backend.hcl.example`:

```hcl
# Copy to backend.hcl (gitignored). Set bucket to the bootstrap stack's
# state_bucket_name output, then run:
#   terraform init -backend-config=backend.hcl
bucket       = "ka-tfstate-000000000000"
key          = "foundation/terraform.tfstate"
region       = "us-east-1"
use_lockfile = true
encrypt      = true
```

`terraform/foundation/data.tf`:

```hcl
data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

data "aws_availability_zones" "available" {
  state = "available"
}
```

`terraform/foundation/variables.tf` (later tasks append to this file):

```hcl
# ---- common ----

variable "aws_region" {
  description = "AWS region for every resource in this stack."
  type        = string
}

variable "allowed_account_ids" {
  description = "AWS account IDs Terraform may operate on; any other active credentials are refused. Private: set in private.auto.tfvars."
  type        = list(string)

  validation {
    condition     = length(var.allowed_account_ids) > 0 && alltrue([for id in var.allowed_account_ids : can(regex("^[0-9]{12}$", id))])
    error_message = "allowed_account_ids must list one or more 12-digit AWS account IDs."
  }
}

variable "name_prefix" {
  description = "Prefix for every resource name."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,14}$", var.name_prefix))
    error_message = "name_prefix must be 2-15 lowercase letters, digits or hyphens, starting with a letter."
  }
}

variable "tags" {
  description = "Tags applied to every resource through the provider's default_tags. Must include Project, ManagedBy and Stack."
  type        = map(string)

  validation {
    condition     = alltrue([for k in ["Project", "ManagedBy", "Stack"] : contains(keys(var.tags), k)])
    error_message = "tags must include Project, ManagedBy and Stack."
  }
}

# ---- network ----

variable "vpc_cidr" {
  description = "CIDR block of the VPC."
  type        = string

  validation {
    condition     = can(cidrnetmask(var.vpc_cidr))
    error_message = "vpc_cidr must be a valid IPv4 CIDR block."
  }
}

variable "subnet_count" {
  description = "Number of public subnets, one per availability zone. EKS needs at least two."
  type        = number

  validation {
    condition     = var.subnet_count >= 2
    error_message = "subnet_count must be at least 2: EKS needs subnets in two availability zones."
  }
}

variable "subnet_newbits" {
  description = "Bits added to the VPC prefix for each subnet (4 turns a /16 into /20s)."
  type        = number

  validation {
    condition     = var.subnet_newbits >= 1 && var.subnet_newbits <= 12
    error_message = "subnet_newbits must be between 1 and 12."
  }
}

variable "excluded_az_ids" {
  description = "Availability zone IDs never to place subnets in, such as zones where EKS cannot run a control plane."
  type        = list(string)
}

variable "opensearch_ingress_cidrs" {
  description = "CIDR blocks allowed to reach the OpenSearch domain over HTTPS."
  type        = list(string)

  validation {
    condition     = alltrue([for c in var.opensearch_ingress_cidrs : can(cidrnetmask(c))])
    error_message = "Every opensearch_ingress_cidrs entry must be a valid IPv4 CIDR block."
  }
}
```

- [ ] **Step 4: Implement the network**

`terraform/foundation/network.tf`:

```hcl
locals {
  # EKS cannot place a control plane in some zones, so zones are filtered by
  # zone ID (stable across accounts, unlike zone names) before the first
  # subnet_count are used.
  usable_azs = [
    for i, name in data.aws_availability_zones.available.names : name
    if !contains(var.excluded_az_ids, data.aws_availability_zones.available.zone_ids[i])
  ]
  subnet_azs = slice(local.usable_azs, 0, min(var.subnet_count, length(local.usable_azs)))
}

resource "aws_vpc" "main" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = {
    Name = "${var.name_prefix}-vpc"
  }

  lifecycle {
    precondition {
      condition     = length(local.usable_azs) >= var.subnet_count
      error_message = "Too few availability zones remain after excluded_az_ids for subnet_count subnets."
    }
  }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name = "${var.name_prefix}-igw"
  }
}

# Public subnets only: there is no NAT gateway, so nodes need public IPs to
# reach ECR and Bedrock.
resource "aws_subnet" "public" {
  count = length(local.subnet_azs)

  vpc_id                  = aws_vpc.main.id
  availability_zone       = local.subnet_azs[count.index]
  cidr_block              = cidrsubnet(var.vpc_cidr, var.subnet_newbits, count.index)
  map_public_ip_on_launch = true

  tags = {
    Name                     = "${var.name_prefix}-public-${local.subnet_azs[count.index]}"
    "kubernetes.io/role/elb" = "1"
  }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  tags = {
    Name = "${var.name_prefix}-public"
  }
}

resource "aws_route_table_association" "public" {
  count = length(aws_subnet.public)

  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

# The daily OpenSearch domain is reachable only from inside the VPC, which is
# why chat-api can keep using an unsigned client. Terraform removes the default
# allow-all egress rule; the domain never initiates connections.
resource "aws_security_group" "opensearch" {
  name        = "${var.name_prefix}-opensearch"
  description = "HTTPS to the OpenSearch domain from inside the VPC only"
  vpc_id      = aws_vpc.main.id

  tags = {
    Name = "${var.name_prefix}-opensearch"
  }
}

resource "aws_vpc_security_group_ingress_rule" "opensearch_https" {
  for_each = toset(var.opensearch_ingress_cidrs)

  security_group_id = aws_security_group.opensearch.id
  description       = "HTTPS from ${each.value}"
  cidr_ipv4         = each.value
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
}
```

- [ ] **Step 5: Add the configuration values**

`terraform/foundation/terraform.tfvars` (later tasks append):

```hcl
# ---- common ----
aws_region  = "us-east-1"
name_prefix = "ka"

tags = {
  Project   = "knowledge-assistant"
  ManagedBy = "terraform"
  Stack     = "foundation"
}

# ---- network ----
vpc_cidr       = "10.40.0.0/16"
subnet_count   = 2
subnet_newbits = 4
# use1-az3 cannot host an EKS control plane.
excluded_az_ids          = ["use1-az3"]
opensearch_ingress_cidrs = ["10.40.0.0/16"]
```

`terraform/foundation/private.auto.tfvars.example` (Task 6 appends to it):

```hcl
# Copy to private.auto.tfvars (gitignored) and fill in. Never commit the copy:
# the repository is public.
#   aws sts get-caller-identity --query Account --output text
allowed_account_ids = ["000000000000"]
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `terraform fmt -recursive terraform && terraform -chdir=terraform/foundation init -backend=false && terraform -chdir=terraform/foundation test`
Expected: `Success! 3 passed, 0 failed.`

- [ ] **Step 7: Run all checks**

Run: `make tf-check`
Expected: `All Terraform checks passed.`

- [ ] **Step 8: Checkpoint**

Run `git status --short`. Confirm there is no `backend.hcl` or `private.auto.tfvars`, only the `.example` files. Do not commit.

---

### Task 4: Storage, images and the Claude key secret

**Files:**
- Modify: `terraform/foundation/variables.tf` (append)
- Create: `terraform/foundation/storage.tf`, `terraform/foundation/secrets.tf`
- Modify: `terraform/foundation/terraform.tfvars` (append)
- Modify: `terraform/check.sh` (add the no-secret-version guard)
- Test: `terraform/foundation/tests/foundation.tftest.hcl` (append)

**Interfaces:**
- Consumes: `data.aws_caller_identity.current` (Task 3), module `private-bucket` (Task 2).
- Produces: `module.docs_bucket` (outputs `name` and `arn`), `aws_ecr_repository.app` (for_each keyed by repository name), and `aws_secretsmanager_secret.anthropic` (attributes `arn` and `name`).

- [ ] **Step 1: Extend the test**

In `terraform/foundation/tests/foundation.tftest.hcl`, add to the `variables` block:

```hcl
  # storage and secrets
  docs_noncurrent_version_days = 30
  ecr_repositories             = ["zz/one", "zz/two"]
  ecr_image_tag_mutability     = "MUTABLE"
  ecr_scan_on_push             = true
  ecr_keep_tagged_images       = 10
  ecr_untagged_expiry_days     = 1
  anthropic_secret_name        = "zz/anthropic"
  secret_recovery_window_days  = 7
```

Append these run blocks:

```hcl
run "docs_bucket_is_named_from_prefix_and_account" {
  command = plan

  assert {
    condition     = module.docs_bucket.name == "zz-docs-123456789012"
    error_message = "The docs bucket must be named <name_prefix>-docs-<account-id>."
  }
}

run "ecr_repositories_scan_and_expire_old_images" {
  command = plan

  assert {
    condition     = keys(aws_ecr_repository.app) == ["zz/one", "zz/two"]
    error_message = "There must be one repository per ecr_repositories entry."
  }

  assert {
    condition = alltrue([
      for r in aws_ecr_repository.app :
      r.image_tag_mutability == "MUTABLE" && one(r.image_scanning_configuration).scan_on_push
    ])
    error_message = "Repositories must take tag mutability and scan-on-push from tfvars."
  }

  assert {
    condition = alltrue([
      for p in aws_ecr_lifecycle_policy.app :
      jsondecode(p.policy).rules[0].selection.tagStatus == "untagged" &&
      jsondecode(p.policy).rules[0].selection.countNumber == 1 &&
      jsondecode(p.policy).rules[1].selection.tagStatus == "tagged" &&
      jsondecode(p.policy).rules[1].selection.countNumber == 10
    ])
    error_message = "Lifecycle must expire untagged images after ecr_untagged_expiry_days and keep ecr_keep_tagged_images tagged ones."
  }
}

run "secret_is_created_with_its_recovery_window" {
  command = plan

  assert {
    condition     = aws_secretsmanager_secret.anthropic.name == "zz/anthropic"
    error_message = "The secret name must come from anthropic_secret_name."
  }

  assert {
    condition     = aws_secretsmanager_secret.anthropic.recovery_window_in_days == 7
    error_message = "The recovery window must come from secret_recovery_window_days."
  }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `terraform -chdir=terraform/foundation test`
Expected: FAIL. The storage and secret variables are undeclared, and `module.docs_bucket`, `aws_ecr_repository.app` and `aws_secretsmanager_secret.anthropic` do not exist.

- [ ] **Step 3: Declare the variables**

Append to `terraform/foundation/variables.tf`:

```hcl
# ---- storage and secrets ----

variable "docs_noncurrent_version_days" {
  description = "Days to keep noncurrent versions of documents in the docs bucket."
  type        = number

  validation {
    condition     = var.docs_noncurrent_version_days >= 1
    error_message = "docs_noncurrent_version_days must be at least 1."
  }
}

variable "ecr_repositories" {
  description = "ECR repository names, one per container image."
  type        = set(string)

  validation {
    condition     = length(var.ecr_repositories) > 0
    error_message = "ecr_repositories must name at least one repository."
  }
}

variable "ecr_image_tag_mutability" {
  description = "Whether image tags can be overwritten."
  type        = string

  validation {
    condition     = can(regex("^(IMMUTABLE|MUTABLE)$", var.ecr_image_tag_mutability))
    error_message = "ecr_image_tag_mutability must be MUTABLE or IMMUTABLE."
  }
}

variable "ecr_scan_on_push" {
  description = "Scan each image for vulnerabilities when it is pushed."
  type        = bool
}

variable "ecr_keep_tagged_images" {
  description = "Number of most recent tagged images each repository keeps."
  type        = number

  validation {
    condition     = var.ecr_keep_tagged_images >= 1
    error_message = "ecr_keep_tagged_images must be at least 1."
  }
}

variable "ecr_untagged_expiry_days" {
  description = "Days after which untagged images expire."
  type        = number

  validation {
    condition     = var.ecr_untagged_expiry_days >= 1
    error_message = "ecr_untagged_expiry_days must be at least 1."
  }
}

variable "anthropic_secret_name" {
  description = "Secrets Manager name of the Anthropic API key chat-api reads at startup."
  type        = string
}

variable "secret_recovery_window_days" {
  description = "Days a deleted secret can still be restored: 0, or 7 to 30."
  type        = number

  validation {
    condition     = var.secret_recovery_window_days == 0 || (var.secret_recovery_window_days >= 7 && var.secret_recovery_window_days <= 30)
    error_message = "secret_recovery_window_days must be 0, or between 7 and 30."
  }
}
```

- [ ] **Step 4: Implement storage and the secret**

`terraform/foundation/storage.tf`:

```hcl
# Documents live under one prefix per team (coupa/, star/, hr/); the ingestion
# job reads them from here every morning to refill the day's empty index.
module "docs_bucket" {
  source = "../modules/private-bucket"

  name                    = "${var.name_prefix}-docs-${data.aws_caller_identity.current.account_id}"
  noncurrent_version_days = var.docs_noncurrent_version_days
}

# Not force-deleted: destroying a repository that still holds images fails
# rather than losing them.
resource "aws_ecr_repository" "app" {
  for_each = var.ecr_repositories

  name                 = each.value
  image_tag_mutability = var.ecr_image_tag_mutability

  image_scanning_configuration {
    scan_on_push = var.ecr_scan_on_push
  }
}

resource "aws_ecr_lifecycle_policy" "app" {
  for_each = aws_ecr_repository.app

  repository = each.value.name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Expire untagged images"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = var.ecr_untagged_expiry_days
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "Keep only the most recent tagged images"
        selection = {
          tagStatus      = "tagged"
          tagPatternList = ["*"]
          countType      = "imageCountMoreThan"
          countNumber    = var.ecr_keep_tagged_images
        }
        action = { type = "expire" }
      },
    ]
  })
}
```

`terraform/foundation/secrets.tf`:

```hcl
# Terraform creates the secret but never its value: there is deliberately no
# aws_secretsmanager_secret_version, so the key never enters state or git. The
# owner sets it with `aws secretsmanager put-secret-value`, and chat-api reads
# it at startup with its pod role.
resource "aws_secretsmanager_secret" "anthropic" {
  name                    = var.anthropic_secret_name
  description             = "Anthropic API key, read by chat-api at startup"
  recovery_window_in_days = var.secret_recovery_window_days

  lifecycle {
    prevent_destroy = true
  }
}
```

- [ ] **Step 5: Add the configuration values**

Append to `terraform/foundation/terraform.tfvars`:

```hcl

# ---- storage and secrets ----
docs_noncurrent_version_days = 30

# The image names deploy/k8s/*.yaml already use.
ecr_repositories = [
  "knowledge-assistant/chat-api",
  "knowledge-assistant/ingestion",
  "knowledge-assistant/ui",
]
ecr_image_tag_mutability = "MUTABLE"
ecr_scan_on_push         = true
ecr_keep_tagged_images   = 10
ecr_untagged_expiry_days = 1

anthropic_secret_name       = "ka/anthropic-api-key"
secret_recovery_window_days = 7
```

- [ ] **Step 6: Guard against the key ever entering state**

In `terraform/check.sh`, insert before the `echo "== script tests"` line:

```bash
echo "== the Claude key never enters Terraform"
if grep -rn --include='*.tf' 'aws_secretsmanager_secret_version' "$root"; then
  echo "aws_secretsmanager_secret_version would put the key in state; set it with put-secret-value instead." >&2
  exit 1
fi
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `terraform fmt -recursive terraform && terraform -chdir=terraform/foundation init -backend=false && terraform -chdir=terraform/foundation test`
Expected: `Success! 6 passed, 0 failed.`

- [ ] **Step 8: Run all checks**

Run: `make tf-check`
Expected: `All Terraform checks passed.`

- [ ] **Step 9: Checkpoint**

Run `git status --short` and confirm the changes are limited to this task's files. Do not commit.

---

### Task 5: IAM for EKS, Pod Identity and OpenSearch

**Files:**
- Create: `terraform/foundation/policies/aws-lb-controller-v3.5.0.json` (downloaded)
- Modify: `terraform/foundation/variables.tf` (append)
- Create: `terraform/foundation/iam.tf`
- Modify: `terraform/foundation/terraform.tfvars` (append)
- Test: `terraform/foundation/tests/foundation.tftest.hcl` (append)

**Interfaces:**
- Consumes: `data.aws_partition.current` (Task 3), `module.docs_bucket.arn` and `aws_secretsmanager_secret.anthropic.arn` (Task 4).
- Produces: `aws_iam_role.eks_cluster`, `aws_iam_role.eks_node`, `aws_iam_role.chat_api`, `aws_iam_role.ingestion` and `aws_iam_role.aws_lb_controller` (each exposes `.arn`), plus `aws_iam_service_linked_role.opensearch`.

- [ ] **Step 1: Vendor the Load Balancer Controller policy**

```bash
mkdir -p terraform/foundation/policies
curl -fsSL https://raw.githubusercontent.com/kubernetes-sigs/aws-load-balancer-controller/v3.5.0/docs/install/iam_policy.json \
  -o terraform/foundation/policies/aws-lb-controller-v3.5.0.json
echo "16f232c9d9f79366fe949c4550ad517a202380058a9e48d45a4e215044a20a6a  terraform/foundation/policies/aws-lb-controller-v3.5.0.json" | shasum -a 256 -c
```

Expected: `terraform/foundation/policies/aws-lb-controller-v3.5.0.json: OK`. If the checksum differs, stop and report: the upstream file changed and needs review before it is trusted.

- [ ] **Step 2: Extend the test**

Add to the `variables` block:

```hcl
  # iam
  chat_api_bedrock_model_ids  = ["zz.embed-v1", "zz.chat-v1"]
  ingestion_bedrock_model_ids = ["zz.embed-v1"]
  lb_controller_policy_file   = "policies/aws-lb-controller-v3.5.0.json"
```

Append these run blocks:

```hcl
run "pod_roles_trust_pod_identity_not_a_cluster" {
  command = plan

  assert {
    condition = alltrue([
      for r in [aws_iam_role.chat_api, aws_iam_role.ingestion, aws_iam_role.aws_lb_controller] :
      jsondecode(r.assume_role_policy).Statement[0].Principal.Service == "pods.eks.amazonaws.com" &&
      toset(jsondecode(r.assume_role_policy).Statement[0].Action) == toset(["sts:AssumeRole", "sts:TagSession"])
    ])
    error_message = "Pod roles must trust pods.eks.amazonaws.com for AssumeRole and TagSession."
  }

  assert {
    condition     = aws_iam_role.chat_api.name == "zz-chat-api"
    error_message = "Role names must be built from name_prefix."
  }
}

run "chat_api_invokes_only_configured_models_and_reads_its_secret" {
  command = plan

  assert {
    condition = jsondecode(aws_iam_role_policy.chat_api.policy).Statement[0].Resource == [
      "arn:aws:bedrock:us-east-1::foundation-model/zz.embed-v1",
      "arn:aws:bedrock:us-east-1::foundation-model/zz.chat-v1",
    ]
    error_message = "chat-api must be limited to chat_api_bedrock_model_ids."
  }

  assert {
    condition     = jsondecode(aws_iam_role_policy.chat_api.policy).Statement[1].Resource == aws_secretsmanager_secret.anthropic.arn
    error_message = "chat-api must read only the Claude key secret."
  }

  assert {
    condition     = !strcontains(aws_iam_role_policy.chat_api.policy, "s3:")
    error_message = "chat-api must have no S3 access."
  }
}

run "ingestion_reads_docs_and_embeds_only" {
  command = plan

  assert {
    condition     = jsondecode(aws_iam_role_policy.ingestion.policy).Statement[0].Resource == module.docs_bucket.arn
    error_message = "ingestion must list only the docs bucket."
  }

  assert {
    condition     = jsondecode(aws_iam_role_policy.ingestion.policy).Statement[1].Resource == "${module.docs_bucket.arn}/*"
    error_message = "ingestion must read only objects in the docs bucket."
  }

  assert {
    condition     = jsondecode(aws_iam_role_policy.ingestion.policy).Statement[2].Resource == ["arn:aws:bedrock:us-east-1::foundation-model/zz.embed-v1"]
    error_message = "ingestion may invoke only ingestion_bedrock_model_ids."
  }

  assert {
    condition     = !strcontains(aws_iam_role_policy.ingestion.policy, "secretsmanager:")
    error_message = "ingestion must not read secrets."
  }
}

run "eks_roles_carry_their_managed_policies" {
  command = plan

  assert {
    condition     = aws_iam_role_policy_attachment.eks_cluster.policy_arn == "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
    error_message = "The cluster role needs AmazonEKSClusterPolicy."
  }

  assert {
    condition = toset([for a in aws_iam_role_policy_attachment.eks_node : a.policy_arn]) == toset([
      "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
      "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
      "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
    ])
    error_message = "The node role needs the worker, CNI and ECR read-only policies."
  }
}

run "lb_controller_policy_is_the_vendored_file" {
  command = plan

  assert {
    condition     = jsondecode(aws_iam_policy.aws_lb_controller.policy) == jsondecode(file("${path.module}/policies/aws-lb-controller-v3.5.0.json"))
    error_message = "The controller policy must be read from lb_controller_policy_file."
  }
}

run "opensearch_service_linked_role_is_created" {
  command = plan

  assert {
    condition     = aws_iam_service_linked_role.opensearch.aws_service_name == "opensearchservice.amazonaws.com"
    error_message = "The OpenSearch service-linked role must be created; VPC domains fail without it."
  }
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `terraform -chdir=terraform/foundation test`
Expected: FAIL. The IAM variables and resources are undeclared.

- [ ] **Step 4: Declare the variables**

Append to `terraform/foundation/variables.tf`:

```hcl
# ---- iam ----

variable "chat_api_bedrock_model_ids" {
  description = "Bedrock foundation model IDs chat-api may invoke (embedding, rerank, rewrite)."
  type        = list(string)

  validation {
    condition     = length(var.chat_api_bedrock_model_ids) > 0
    error_message = "chat_api_bedrock_model_ids must list at least one model."
  }
}

variable "ingestion_bedrock_model_ids" {
  description = "Bedrock foundation model IDs the ingestion job may invoke (embedding only)."
  type        = list(string)

  validation {
    condition     = length(var.ingestion_bedrock_model_ids) > 0
    error_message = "ingestion_bedrock_model_ids must list at least one model."
  }
}

variable "lb_controller_policy_file" {
  description = "Path, relative to this stack, of the vendored AWS Load Balancer Controller IAM policy. Its version must match the controller the daily stack installs."
  type        = string
}
```

- [ ] **Step 5: Implement IAM**

`terraform/foundation/iam.tf`:

```hcl
# Every IAM role the daily stack needs lives here, so the nightly destroy never
# touches IAM and mornings never wait for new IAM to take effect.

locals {
  bedrock_model_arn_prefix = "arn:${data.aws_partition.current.partition}:bedrock:${var.aws_region}::foundation-model"
  managed_policy_prefix    = "arn:${data.aws_partition.current.partition}:iam::aws:policy"

  # Pod Identity: roles trust the EKS pods service rather than one cluster's
  # OIDC issuer (IRSA), so they survive the cluster being recreated daily.
  pod_identity_trust = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "pods.eks.amazonaws.com" }
      Action    = ["sts:AssumeRole", "sts:TagSession"]
    }]
  })
}

# ---- EKS ----

resource "aws_iam_role" "eks_cluster" {
  name = "${var.name_prefix}-eks-cluster"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "eks.amazonaws.com" }
      Action    = ["sts:AssumeRole", "sts:TagSession"]
    }]
  })
}

resource "aws_iam_role_policy_attachment" "eks_cluster" {
  role       = aws_iam_role.eks_cluster.name
  policy_arn = "${local.managed_policy_prefix}/AmazonEKSClusterPolicy"
}

resource "aws_iam_role" "eks_node" {
  name = "${var.name_prefix}-eks-node"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "eks_node" {
  for_each = toset([
    "AmazonEKSWorkerNodePolicy",
    "AmazonEKS_CNI_Policy",
    "AmazonEC2ContainerRegistryReadOnly",
  ])

  role       = aws_iam_role.eks_node.name
  policy_arn = "${local.managed_policy_prefix}/${each.value}"
}

# ---- pods ----

resource "aws_iam_role" "chat_api" {
  name               = "${var.name_prefix}-chat-api"
  assume_role_policy = local.pod_identity_trust
}

resource "aws_iam_role_policy" "chat_api" {
  name = "bedrock-and-claude-key"
  role = aws_iam_role.chat_api.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "InvokeConfiguredModels"
        Effect   = "Allow"
        Action   = ["bedrock:InvokeModel", "bedrock:InvokeModelWithResponseStream"]
        Resource = [for id in var.chat_api_bedrock_model_ids : "${local.bedrock_model_arn_prefix}/${id}"]
      },
      {
        Sid      = "ReadClaudeKey"
        Effect   = "Allow"
        Action   = "secretsmanager:GetSecretValue"
        Resource = aws_secretsmanager_secret.anthropic.arn
      },
    ]
  })
}

resource "aws_iam_role" "ingestion" {
  name               = "${var.name_prefix}-ingestion"
  assume_role_policy = local.pod_identity_trust
}

resource "aws_iam_role_policy" "ingestion" {
  name = "docs-and-embeddings"
  role = aws_iam_role.ingestion.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "ListDocs"
        Effect   = "Allow"
        Action   = "s3:ListBucket"
        Resource = module.docs_bucket.arn
      },
      {
        Sid      = "ReadDocs"
        Effect   = "Allow"
        Action   = "s3:GetObject"
        Resource = "${module.docs_bucket.arn}/*"
      },
      {
        Sid      = "InvokeEmbeddingModels"
        Effect   = "Allow"
        Action   = "bedrock:InvokeModel"
        Resource = [for id in var.ingestion_bedrock_model_ids : "${local.bedrock_model_arn_prefix}/${id}"]
      },
    ]
  })
}

resource "aws_iam_role" "aws_lb_controller" {
  name               = "${var.name_prefix}-aws-lb-controller"
  assume_role_policy = local.pod_identity_trust
}

# Vendored from the controller release; the file and the controller version the
# daily stack installs change together.
resource "aws_iam_policy" "aws_lb_controller" {
  name        = "${var.name_prefix}-aws-lb-controller"
  description = "AWS Load Balancer Controller permissions, vendored from the controller release"
  policy      = file("${path.module}/${var.lb_controller_policy_file}")
}

resource "aws_iam_role_policy_attachment" "aws_lb_controller" {
  role       = aws_iam_role.aws_lb_controller.name
  policy_arn = aws_iam_policy.aws_lb_controller.arn
}

# ---- OpenSearch ----

# A VPC OpenSearch domain cannot be created without this role, and the account
# does not have it. The EKS, node group, ELB and Auto Scaling service-linked
# roles already exist and are not managed here.
resource "aws_iam_service_linked_role" "opensearch" {
  aws_service_name = "opensearchservice.amazonaws.com"
}
```

- [ ] **Step 6: Add the configuration values**

Append to `terraform/foundation/terraform.tfvars`:

```hcl

# ---- iam ----
# Titan v2 for embeddings; gpt-oss-20b for rerank and query rewrite.
chat_api_bedrock_model_ids = [
  "amazon.titan-embed-text-v2:0",
  "openai.gpt-oss-20b-1:0",
]
ingestion_bedrock_model_ids = ["amazon.titan-embed-text-v2:0"]
lb_controller_policy_file   = "policies/aws-lb-controller-v3.5.0.json"
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `terraform fmt -recursive terraform && terraform -chdir=terraform/foundation test`
Expected: `Success! 12 passed, 0 failed.`

- [ ] **Step 8: Run all checks**

Run: `make tf-check`
Expected: `All Terraform checks passed.`

- [ ] **Step 9: Checkpoint**

Run `git status --short`. Do not commit.

---

### Task 6: Budget and outputs

**Files:**
- Modify: `terraform/foundation/variables.tf` (append)
- Create: `terraform/foundation/budget.tf`, `terraform/foundation/outputs.tf`
- Modify: `terraform/foundation/terraform.tfvars` (append), `terraform/foundation/private.auto.tfvars.example` (append)
- Test: `terraform/foundation/tests/foundation.tftest.hcl` (append)

**Interfaces:**
- Consumes: everything from Tasks 3–5.
- Produces: outputs `aws_region`, `vpc_id`, `subnet_ids`, `opensearch_security_group_id`, `eks_cluster_role_arn`, `eks_node_role_arn`, `pod_role_arns` (map with keys `chat-api`, `ingestion`, `aws-lb-controller`), `admin_principal_arn`, `docs_bucket`, `ecr_repository_urls` (map of repository name to URL), `anthropic_secret_arn`, `anthropic_secret_name`, and `verify_expectations`, an object with `budget_name`, `budget_limit_usd`, `budget_notification_count`, `project_tag`, `chat_api_model_ids` and `ingestion_model_ids`. Task 7's `verify.sh` reads exactly these keys.

- [ ] **Step 1: Extend the test**

Add to the `variables` block:

```hcl
  # budget
  budget_limit_usd               = 50
  budget_time_unit               = "MONTHLY"
  budget_actual_thresholds_pct   = [50, 80, 100]
  budget_forecast_thresholds_pct = [100]
  alert_emails                   = ["alerts@example.test"]
```

Append these run blocks:

```hcl
run "budget_alerts_at_each_configured_threshold" {
  command = plan

  assert {
    condition     = aws_budgets_budget.monthly.name == "zz-monthly"
    error_message = "The budget name must be <name_prefix>-<time unit>."
  }

  assert {
    condition     = aws_budgets_budget.monthly.limit_amount == "50.0" && aws_budgets_budget.monthly.time_unit == "MONTHLY"
    error_message = "The limit and period must come from tfvars."
  }

  assert {
    condition = sort([
      for n in aws_budgets_budget.monthly.notification : "${n.notification_type}:${n.threshold}"
    ]) == sort(["ACTUAL:50", "ACTUAL:80", "ACTUAL:100", "FORECASTED:100"])
    error_message = "There must be one notification per actual and forecast threshold."
  }

  assert {
    condition     = alltrue([for n in aws_budgets_budget.monthly.notification : n.subscriber_email_addresses == toset(["alerts@example.test"])])
    error_message = "Every notification must go to alert_emails."
  }
}

run "rejects_an_invalid_alert_email" {
  command = plan

  variables {
    alert_emails = ["not-an-email"]
  }

  expect_failures = [var.alert_emails]
}

run "outputs_expose_what_the_daily_stack_and_verify_need" {
  command = plan

  assert {
    condition     = output.admin_principal_arn == "arn:aws:iam::123456789012:user/tester"
    error_message = "admin_principal_arn must be the identity applying the stack."
  }

  assert {
    condition     = keys(output.pod_role_arns) == ["aws-lb-controller", "chat-api", "ingestion"]
    error_message = "pod_role_arns must map every pod role."
  }

  assert {
    condition     = length(output.subnet_ids) == 2 && output.docs_bucket == "zz-docs-123456789012"
    error_message = "Network and docs outputs must be present."
  }

  assert {
    condition     = keys(output.ecr_repository_urls) == ["zz/one", "zz/two"]
    error_message = "ecr_repository_urls must map every repository."
  }

  assert {
    condition     = output.verify_expectations.budget_notification_count == 4 && output.verify_expectations.project_tag == "p"
    error_message = "verify_expectations must reflect the tfvars that verify.sh checks against."
  }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `terraform -chdir=terraform/foundation test`
Expected: FAIL. The budget variables, `aws_budgets_budget.monthly` and the outputs do not exist.

- [ ] **Step 3: Declare the variables**

Append to `terraform/foundation/variables.tf`:

```hcl
# ---- budget ----

variable "budget_limit_usd" {
  description = "Budget limit in US dollars for each budget period."
  type        = number

  validation {
    condition     = var.budget_limit_usd > 0
    error_message = "budget_limit_usd must be positive."
  }
}

variable "budget_time_unit" {
  description = "Budget period."
  type        = string

  validation {
    condition     = can(regex("^(DAILY|MONTHLY|QUARTERLY|ANNUALLY)$", var.budget_time_unit))
    error_message = "budget_time_unit must be DAILY, MONTHLY, QUARTERLY or ANNUALLY."
  }
}

variable "budget_actual_thresholds_pct" {
  description = "Percentages of the limit at which actual spend sends an alert."
  type        = list(number)

  validation {
    condition     = alltrue([for t in var.budget_actual_thresholds_pct : t > 0])
    error_message = "Every threshold must be positive."
  }
}

variable "budget_forecast_thresholds_pct" {
  description = "Percentages of the limit at which forecast spend sends an alert."
  type        = list(number)

  validation {
    condition     = alltrue([for t in var.budget_forecast_thresholds_pct : t > 0])
    error_message = "Every threshold must be positive."
  }
}

variable "alert_emails" {
  description = "Addresses that receive budget alerts (at most 10). Private: set in private.auto.tfvars."
  type        = list(string)

  validation {
    condition = (
      length(var.alert_emails) > 0 && length(var.alert_emails) <= 10 &&
      alltrue([for e in var.alert_emails : can(regex("^[^@[:space:]]+@[^@[:space:]]+\\.[^@[:space:]]+$", e))])
    )
    error_message = "alert_emails must list 1 to 10 valid email addresses."
  }
}
```

- [ ] **Step 4: Implement the budget**

`terraform/foundation/budget.tf`:

```hcl
# Account-wide on purpose: other spend in the account is small, and orphaned
# resources (a load balancer left behind by a failed teardown) may not carry
# project tags, so a tag-filtered budget would miss exactly the costs it exists
# to catch.
resource "aws_budgets_budget" "monthly" {
  name        = "${var.name_prefix}-${lower(var.budget_time_unit)}"
  budget_type = "COST"
  # One decimal place, the form the Budgets API returns, so plans stay clean.
  limit_amount = format("%.1f", var.budget_limit_usd)
  limit_unit   = "USD"
  time_unit    = var.budget_time_unit

  dynamic "notification" {
    for_each = var.budget_actual_thresholds_pct

    content {
      comparison_operator        = "GREATER_THAN"
      threshold                  = notification.value
      threshold_type             = "PERCENTAGE"
      notification_type          = "ACTUAL"
      subscriber_email_addresses = var.alert_emails
    }
  }

  dynamic "notification" {
    for_each = var.budget_forecast_thresholds_pct

    content {
      comparison_operator        = "GREATER_THAN"
      threshold                  = notification.value
      threshold_type             = "PERCENTAGE"
      notification_type          = "FORECASTED"
      subscriber_email_addresses = var.alert_emails
    }
  }
}
```

- [ ] **Step 5: Implement the outputs**

`terraform/foundation/outputs.tf`:

```hcl
# Read by the daily stack through terraform_remote_state, and by verify.sh
# through `terraform output -json`.

output "aws_region" {
  description = "Region of every foundation resource."
  value       = var.aws_region
}

output "vpc_id" {
  description = "VPC the daily cluster and OpenSearch domain run in."
  value       = aws_vpc.main.id
}

output "subnet_ids" {
  description = "Public subnets, one per availability zone."
  value       = aws_subnet.public[*].id
}

output "opensearch_security_group_id" {
  description = "Security group to attach to the daily OpenSearch domain."
  value       = aws_security_group.opensearch.id
}

output "eks_cluster_role_arn" {
  description = "Role the EKS control plane assumes."
  value       = aws_iam_role.eks_cluster.arn
}

output "eks_node_role_arn" {
  description = "Role the EKS worker nodes assume."
  value       = aws_iam_role.eks_node.arn
}

output "pod_role_arns" {
  description = "Pod Identity roles, keyed by the workload that assumes them."
  value = {
    "chat-api"          = aws_iam_role.chat_api.arn
    "ingestion"         = aws_iam_role.ingestion.arn
    "aws-lb-controller" = aws_iam_role.aws_lb_controller.arn
  }
}

output "admin_principal_arn" {
  description = "Identity that applied the foundation; the daily stack grants it cluster admin."
  value       = data.aws_caller_identity.current.arn
}

output "docs_bucket" {
  description = "Bucket holding the document corpus, one prefix per team."
  value       = module.docs_bucket.name
}

output "ecr_repository_urls" {
  description = "Repository URL for each image, keyed by repository name."
  value       = { for name, repo in aws_ecr_repository.app : name => repo.repository_url }
}

output "anthropic_secret_arn" {
  description = "Secret holding the Anthropic API key."
  value       = aws_secretsmanager_secret.anthropic.arn
}

output "anthropic_secret_name" {
  description = "Name to pass to `aws secretsmanager put-secret-value --secret-id`."
  value       = aws_secretsmanager_secret.anthropic.name
}

output "verify_expectations" {
  description = "Values verify.sh checks the live account against."
  value = {
    budget_name               = aws_budgets_budget.monthly.name
    budget_limit_usd          = var.budget_limit_usd
    budget_notification_count = length(var.budget_actual_thresholds_pct) + length(var.budget_forecast_thresholds_pct)
    project_tag               = var.tags["Project"]
    chat_api_model_ids        = var.chat_api_bedrock_model_ids
    ingestion_model_ids       = var.ingestion_bedrock_model_ids
  }
}
```

- [ ] **Step 6: Add the configuration values**

Append to `terraform/foundation/terraform.tfvars`:

```hcl

# ---- budget ----
# Account-wide; the expected daily-environment spend is about $60/month, so
# the 100% alert is a deliberate monthly prompt.
budget_limit_usd               = 50
budget_time_unit               = "MONTHLY"
budget_actual_thresholds_pct   = [50, 80, 100]
budget_forecast_thresholds_pct = [100]
```

Append to `terraform/foundation/private.auto.tfvars.example`:

```hcl
alert_emails = ["you@example.com"]
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `terraform fmt -recursive terraform && terraform -chdir=terraform/foundation test`
Expected: `Success! 15 passed, 0 failed.`

- [ ] **Step 8: Run all checks**

Run: `make tf-check`
Expected: `All Terraform checks passed.`

- [ ] **Step 9: Checkpoint**

Run `git status --short`. Do not commit.

---

### Task 7: verify.sh, the Deploy docs and CLAUDE.md

**Files:**
- Create: `terraform/foundation/verify.sh`
- Test: `terraform/foundation/tests/verify_test.sh`, `terraform/foundation/tests/fake-aws/aws`
- Modify: `README.md` (replace the `## Deploy` section)
- Modify: `CLAUDE.md` (Commands and Gotchas)

**Interfaces:**
- Consumes: the Task 6 outputs, read with `terraform output -json`. The environment variable `VERIFY_OUTPUTS_JSON` can point at a file instead, which is how the tests run it.
- Produces: `terraform/foundation/verify.sh` prints `PASS  <check>` or `FAIL  <check> ...` for each check, then `All checks passed.` (exit 0) or `<n> check(s) failed.` (exit 1).

- [ ] **Step 1: Write the stand-in AWS CLI**

Create `terraform/foundation/tests/fake-aws/aws`:

```bash
#!/usr/bin/env bash
# A stand-in for the AWS CLI, just big enough for verify.sh. Policy simulator
# decisions come from $FAKE_AWS_RULES: one "role action resource decision"
# line per allowed call; anything else is implicitDeny, as in AWS.
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
  "iam simulate-principal-policy")
    role=$(arg --policy-source-arn "$@")
    action=$(arg --action-names "$@")
    resource=$(arg --resource-arns "$@")
    decision=$(awk -v r="$role" -v a="$action" -v x="$resource" \
      '$1 == r && $2 == a && $3 == x { print $4; exit }' "$FAKE_AWS_RULES")
    echo "${decision:-implicitDeny}"
    ;;
  "secretsmanager describe-secret")
    if [ "${FAKE_SECRET_EMPTY:-0}" = 1 ]; then echo 'null'; else echo '{"v1":["AWSCURRENT"]}'; fi
    ;;
  "budgets describe-budget")
    printf '{"Budget":{"BudgetLimit":{"Amount":"%s","Unit":"USD"}}}\n' "${FAKE_BUDGET_LIMIT:-50.0}"
    ;;
  "budgets describe-notifications-for-budget")
    jq -n --argjson n "${FAKE_NOTIFICATIONS:-4}" '{Notifications: [range($n) | {NotificationType: "ACTUAL"}]}'
    ;;
  "resourcegroupstaggingapi get-resources")
    echo "${FAKE_TAGGED:-12}"
    ;;
  *)
    echo "fake aws: unsupported call: $*" >&2
    exit 2
    ;;
esac
```

```bash
chmod +x terraform/foundation/tests/fake-aws/aws
```

- [ ] **Step 2: Write the failing verify.sh test**

Create `terraform/foundation/tests/verify_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for verify.sh against the stand-in AWS CLI in tests/fake-aws. The
# point is that verify.sh fails loudly on a missing grant AND on an over-grant;
# a checker that only ever passes is worse than none.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
verify="$here/../verify.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
export PATH="$here/fake-aws:$PATH"
export VERIFY_OUTPUTS_JSON="$tmp/outputs.json"
export FAKE_AWS_RULES="$tmp/rules"
fail() { echo "FAIL: $*"; exit 1; }

chat=arn:aws:iam::123456789012:role/zz-chat-api
ingest=arn:aws:iam::123456789012:role/zz-ingestion
secret=arn:aws:secretsmanager:us-east-1:123456789012:secret:zz/anthropic-AbCdEf
model=arn:aws:bedrock:us-east-1::foundation-model
docs_object=arn:aws:s3:::zz-docs-123456789012/probe.md

cat >"$VERIFY_OUTPUTS_JSON" <<EOF
{
  "aws_region": {"value": "us-east-1"},
  "pod_role_arns": {"value": {"chat-api": "$chat", "ingestion": "$ingest", "aws-lb-controller": "arn:aws:iam::123456789012:role/zz-aws-lb-controller"}},
  "docs_bucket": {"value": "zz-docs-123456789012"},
  "anthropic_secret_arn": {"value": "$secret"},
  "verify_expectations": {"value": {
    "budget_name": "zz-monthly",
    "budget_limit_usd": 50,
    "budget_notification_count": 4,
    "project_tag": "p",
    "chat_api_model_ids": ["zz.embed-v1", "zz.chat-v1"],
    "ingestion_model_ids": ["zz.embed-v1"]
  }}
}
EOF

# Exactly the permissions the spec grants.
grant_spec() {
  cat >"$FAKE_AWS_RULES" <<EOF
$chat bedrock:InvokeModel $model/zz.embed-v1 allowed
$chat bedrock:InvokeModel $model/zz.chat-v1 allowed
$chat secretsmanager:GetSecretValue $secret allowed
$ingest bedrock:InvokeModel $model/zz.embed-v1 allowed
$ingest s3:GetObject $docs_object allowed
EOF
}

grant_spec
"$verify" >"$tmp/out" || fail "the spec's exact permissions did not pass: $(cat "$tmp/out")"
grep -q '^All checks passed.' "$tmp/out" || fail "no success line: $(cat "$tmp/out")"

# A missing grant is caught.
grant_spec
grep -v GetSecretValue "$FAKE_AWS_RULES" >"$tmp/rules.new" && mv "$tmp/rules.new" "$FAKE_AWS_RULES"
if "$verify" >"$tmp/out"; then fail "a missing secret grant passed"; fi
grep -q 'FAIL  chat-api may read the Claude key' "$tmp/out" || fail "missing grant not reported: $(cat "$tmp/out")"

# An over-grant is caught.
grant_spec
echo "$ingest secretsmanager:GetSecretValue $secret allowed" >>"$FAKE_AWS_RULES"
if "$verify" >"$tmp/out"; then fail "an over-grant passed"; fi
grep -q 'FAIL  ingestion may not read the Claude key' "$tmp/out" || fail "over-grant not reported: $(cat "$tmp/out")"

# A secret with no value is caught.
grant_spec
if FAKE_SECRET_EMPTY=1 "$verify" >"$tmp/out"; then fail "an empty secret passed"; fi
grep -q 'FAIL  the Claude key has no value' "$tmp/out" || fail "empty secret not reported"

# A wrong budget is caught.
if FAKE_NOTIFICATIONS=3 "$verify" >"$tmp/out"; then fail "a missing budget notification passed"; fi
if FAKE_BUDGET_LIMIT=40.0 "$verify" >"$tmp/out"; then fail "a wrong budget limit passed"; fi

# Untagged resources are caught.
if FAKE_TAGGED=0 "$verify" >"$tmp/out"; then fail "zero tagged resources passed"; fi

echo "verify.sh: all tests passed"
```

```bash
chmod +x terraform/foundation/tests/verify_test.sh
```

- [ ] **Step 3: Run it to verify it fails**

Run: `bash terraform/foundation/tests/verify_test.sh`
Expected: FAIL, because `verify.sh` does not exist.

- [ ] **Step 4: Implement verify.sh**

Create `terraform/foundation/verify.sh`:

```bash
#!/usr/bin/env bash
# verify.sh checks the applied foundation against its spec: pod-role
# permissions through the IAM policy simulator, that the Claude key has a value
# (never printing it), the budget, and tagging. Every name and ARN comes from
# `terraform output`, so nothing configured in tfvars is repeated here. Run it
# after `terraform apply`, with the credentials Terraform used; re-run it after
# any IAM change.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)

# A Bedrock model neither role is configured for. Both must be denied it.
UNCONFIGURED_MODEL=anthropic.claude-sonnet-4-5-20250929-v1:0

if [ -n "${VERIFY_OUTPUTS_JSON:-}" ]; then
  outputs=$(cat "$VERIFY_OUTPUTS_JSON")
else
  outputs=$(terraform -chdir="$here" output -json)
fi
out() { jq -r "$1" <<<"$outputs"; }

region=$(out '.aws_region.value')
chat_role=$(out '.pod_role_arns.value["chat-api"]')
ingest_role=$(out '.pod_role_arns.value.ingestion')
docs_bucket=$(out '.docs_bucket.value')
secret_arn=$(out '.anthropic_secret_arn.value')
partition=$(cut -d: -f2 <<<"$chat_role")
account_id=$(cut -d: -f5 <<<"$chat_role")
docs_object="arn:${partition}:s3:::${docs_bucket}/probe.md"
model_arn() { echo "arn:${partition}:bedrock:${region}::foundation-model/$1"; }

failures=0
pass() { echo "PASS  $1"; }
fail() {
  echo "FAIL  $1"
  failures=$((failures + 1))
}

# expect <allowed|denied> <role-arn> <action> <resource-arn> <description>
expect() {
  local decision
  decision=$(aws iam simulate-principal-policy --policy-source-arn "$2" \
    --action-names "$3" --resource-arns "$4" \
    --query 'EvaluationResults[0].EvalDecision' --output text)
  if [ "$decision" != allowed ]; then
    decision=denied # implicitDeny and explicitDeny
  fi
  if [ "$decision" = "$1" ]; then
    pass "$5"
  else
    fail "$5 (want $1, got $decision)"
  fi
}

echo "== chat-api"
for id in $(out '.verify_expectations.value.chat_api_model_ids[]'); do
  expect allowed "$chat_role" bedrock:InvokeModel "$(model_arn "$id")" "chat-api may invoke $id"
done
expect denied "$chat_role" bedrock:InvokeModel "$(model_arn "$UNCONFIGURED_MODEL")" \
  "chat-api may not invoke $UNCONFIGURED_MODEL"
expect allowed "$chat_role" secretsmanager:GetSecretValue "$secret_arn" "chat-api may read the Claude key"
expect denied "$chat_role" s3:GetObject "$docs_object" "chat-api may not read documents"

echo "== ingestion"
for id in $(out '.verify_expectations.value.ingestion_model_ids[]'); do
  expect allowed "$ingest_role" bedrock:InvokeModel "$(model_arn "$id")" "ingestion may invoke $id"
done
for id in $(out '.verify_expectations.value | (.chat_api_model_ids - .ingestion_model_ids)[]'); do
  expect denied "$ingest_role" bedrock:InvokeModel "$(model_arn "$id")" "ingestion may not invoke $id"
done
expect allowed "$ingest_role" s3:GetObject "$docs_object" "ingestion may read documents"
expect denied "$ingest_role" secretsmanager:GetSecretValue "$secret_arn" "ingestion may not read the Claude key"

echo "== Claude key"
if aws secretsmanager describe-secret --secret-id "$secret_arn" \
  --query 'VersionIdsToStages' --output json | jq -e 'length > 0' >/dev/null; then
  pass "the Claude key has a value (not printed)"
else
  fail "the Claude key has no value: set it with put-secret-value"
fi

echo "== budget"
budget_name=$(out '.verify_expectations.value.budget_name')
want_limit=$(out '.verify_expectations.value.budget_limit_usd')
want_count=$(out '.verify_expectations.value.budget_notification_count')
limit=$(aws budgets describe-budget --account-id "$account_id" --budget-name "$budget_name" \
  --output json | jq -r '.Budget.BudgetLimit.Amount')
if awk -v a="$limit" -v b="$want_limit" 'BEGIN { exit !(a + 0 == b + 0) }'; then
  pass "budget limit is $want_limit"
else
  fail "budget limit is $limit, want $want_limit"
fi
count=$(aws budgets describe-notifications-for-budget --account-id "$account_id" \
  --budget-name "$budget_name" --output json | jq '.Notifications | length')
if [ "$count" -eq "$want_count" ]; then
  pass "budget has $want_count notifications"
else
  fail "budget has $count notifications, want $want_count"
fi

echo "== tags"
project=$(out '.verify_expectations.value.project_tag')
tagged=$(aws resourcegroupstaggingapi get-resources --region "$region" \
  --tag-filters "Key=Project,Values=$project" \
  --query 'length(ResourceTagMappingList)' --output text)
if [ "$tagged" -gt 0 ]; then
  pass "$tagged resources carry Project=$project"
else
  fail "no resources carry Project=$project"
fi

echo
if [ "$failures" -eq 0 ]; then
  echo "All checks passed."
else
  echo "$failures check(s) failed."
  exit 1
fi
```

```bash
chmod +x terraform/foundation/verify.sh
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `bash terraform/foundation/tests/verify_test.sh`
Expected: `verify.sh: all tests passed`

- [ ] **Step 6: Rewrite the README's Deploy section**

In `README.md`, replace everything from the `## Deploy` heading up to the next `## ` heading (or the end of the file) with:

````markdown
## Deploy

Everything in AWS is managed with Terraform under `terraform/`, run from your
laptop; GitHub Actions has no AWS access. Design:
`docs/superpowers/specs/2026-09-10-aws-foundation-design.md`.

- `terraform/bootstrap` — the S3 bucket that holds Terraform state (its own
  state is local).
- `terraform/foundation` — everything permanent: network, docs bucket, ECR, the
  Claude key secret, IAM roles, budget.
- `terraform/daily` — the environment created each morning and destroyed each
  evening (not built yet).

All configuration is in each stack's `terraform.tfvars` (committed) and
`private.auto.tfvars` (gitignored: account ID, alert email). Change values
there, never in `.tf` files; `make tf-check` fails if a tfvars value appears in
code. `make tf-check` runs every offline check and needs no AWS credentials.

### Prerequisites

```sh
brew uninstall terraform && brew install tfenv
tfenv install 1.5.7 && tfenv use 1.5.7   # the default outside this repo
tfenv install                            # in the repo: reads .terraform-version
```

AWS CLI credentials for the account listed in `allowed_account_ids`.

### One-time setup

```sh
cd terraform/bootstrap
cp private.auto.tfvars.example private.auto.tfvars      # fill in
terraform init && terraform apply
terraform output -raw state_bucket_name

cd ../foundation
cp private.auto.tfvars.example private.auto.tfvars      # fill in
cp backend.hcl.example backend.hcl                      # bucket from above
terraform init -backend-config=backend.hcl
terraform plan -out=foundation.tfplan                   # review: creates only
terraform apply foundation.tfplan
```

Set the Claude API key once. It never reaches Terraform state, git or your
shell history:

```sh
read -rs KEY && aws secretsmanager put-secret-value \
  --secret-id "$(terraform output -raw anthropic_secret_name)" \
  --secret-string "$KEY"; unset KEY
```

Then check the result, and again after any IAM change:

```sh
./verify.sh
```

### Removing the foundation

`prevent_destroy` blocks destroying the state bucket, the docs bucket and the
secret. To remove everything deliberately:

1. Set `prevent_destroy = false` in `terraform/modules/private-bucket/main.tf`
   and `terraform/foundation/secrets.tf` (a local change; do not commit it).
2. Empty the docs bucket, including all object versions (S3 console: Empty).
3. Delete the images in the ECR repositories.
4. `terraform -chdir=terraform/foundation destroy`.
5. Empty the state bucket, including all versions, then
   `terraform -chdir=terraform/bootstrap destroy`.
````

- [ ] **Step 7: Update CLAUDE.md**

In `CLAUDE.md`, under `## Commands`, add after the `- Lint:` line:

```markdown
- Terraform: `make tf-check` runs fmt, validate, `terraform test` (mocked AWS provider) and the script tests; it needs no credentials.
```

Under `## Gotchas`, add as the first bullet:

```markdown
- Terraform lives in `terraform/` and runs only from the owner's laptop. All configuration is in each stack's `terraform.tfvars` / gitignored `private.auto.tfvars`, never literals in `.tf` (`make tf-check` enforces it). Never run `terraform apply`, `destroy`, `import`, or `init` against the real backend unless asked; use `init -backend=false`.
```

- [ ] **Step 8: Run all checks**

Run: `make tf-check`
Expected: `All Terraform checks passed.`, and the output includes `verify.sh: all tests passed`.

- [ ] **Step 9: Checkpoint**

Run `git status --short`. Do not commit. Hand over to the owner for Task 8.

---

### Task 8: Apply and verify (OWNER ONLY)

**Claude does not run any step in this task.** The owner runs it and reports the results.

**Interfaces:**
- Consumes: everything above.
- Produces: the live foundation, and a passing `verify.sh`.

- [ ] **Step 1: Offline checks**

Run: `make tf-check`
Expected: `All Terraform checks passed.`

- [ ] **Step 2: Bootstrap**

```bash
cd terraform/bootstrap
cp private.auto.tfvars.example private.auto.tfvars
# set allowed_account_ids from: aws sts get-caller-identity --query Account --output text
terraform init
terraform plan        # review: creates only
terraform apply
terraform output -raw state_bucket_name
```

- [ ] **Step 3: Foundation**

```bash
cd ../foundation
cp private.auto.tfvars.example private.auto.tfvars   # account ID and alert email
cp backend.hcl.example backend.hcl                   # bucket = the output above
terraform init -backend-config=backend.hcl           # expect: Successfully configured the backend "s3"
terraform plan -out=foundation.tfplan
```

Expected: the plan summary shows `0 to change, 0 to destroy`, and every entry is a create.

While a `terraform plan` is running, run `aws s3 ls s3://<state bucket>/foundation/` in a second terminal. It shows `terraform.tfstate.tflock`, and the file is gone once the plan finishes.

```bash
terraform apply foundation.tfplan
```

- [ ] **Step 4: Set the Claude key**

```bash
read -rs KEY && aws secretsmanager put-secret-value \
  --secret-id "$(terraform output -raw anthropic_secret_name)" \
  --secret-string "$KEY"; unset KEY
```

- [ ] **Step 5: Verify**

Run: `./verify.sh`
Expected: every line starts with `PASS`, followed by `All checks passed.`

Run: `terraform plan`
Expected: `No changes. Your infrastructure matches the configuration.`

- [ ] **Step 6: Report back**

Paste the `verify.sh` output and the second plan's summary line into the conversation. Commit when you choose.
