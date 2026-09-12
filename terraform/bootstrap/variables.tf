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
