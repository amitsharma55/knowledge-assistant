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

# Only the namespaces are configurable. The service-account *names* are a fixed
# contract shared with 4b (and mirror the foundation pod_role_arns keys), so they
# live as literals in pod-identity.tf rather than in tfvars.
variable "chat_api_namespace" {
  description = "Namespace of the chat-api service account (created in 4b)."
  type        = string
}

variable "ingestion_namespace" {
  description = "Namespace of the ingestion service account (created in 4b)."
  type        = string
}

variable "lb_controller_namespace" {
  description = "Namespace of the AWS Load Balancer Controller service account (created in 4b)."
  type        = string
}
