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
