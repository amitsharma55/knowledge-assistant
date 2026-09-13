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
