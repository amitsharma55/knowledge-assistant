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

variable "domain_name" {
  description = "Registered domain whose Route 53 public hosted zone already exists (created by the registrar). The app's ACM certificate is issued and validated within it."
  type        = string
}

variable "app_hostname" {
  description = "Fully-qualified hostname the app is served at over HTTPS; must be within domain_name. deploy.sh points a Route 53 record at the daily ALB for it."
  type        = string

  validation {
    condition     = endswith(var.app_hostname, ".${var.domain_name}")
    error_message = "app_hostname must be a subdomain of domain_name."
  }
}
