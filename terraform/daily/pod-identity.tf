# Bind each foundation pod role to the Kubernetes service account that assumes
# it. The service accounts themselves are created in 4b; Pod Identity binds by
# name, so these can exist first. Service-account names are a fixed 4a/4b
# contract (they mirror the foundation pod_role_arns keys), so they are literals
# here; only the namespaces are configurable. The lb-controller binding is
# created here even though 4b installs the controller, so the IAM wiring stays
# in Terraform.

resource "aws_eks_pod_identity_association" "chat_api" {
  cluster_name    = local.cluster_name
  namespace       = var.chat_api_namespace
  service_account = "chat-api"
  role_arn        = local.foundation.pod_role_arns["chat-api"]

  depends_on = [module.eks]
}

resource "aws_eks_pod_identity_association" "ingestion" {
  cluster_name    = local.cluster_name
  namespace       = var.ingestion_namespace
  service_account = "ingestion"
  role_arn        = local.foundation.pod_role_arns["ingestion"]

  depends_on = [module.eks]
}

resource "aws_eks_pod_identity_association" "lb_controller" {
  cluster_name    = local.cluster_name
  namespace       = var.lb_controller_namespace
  service_account = "aws-load-balancer-controller"
  role_arn        = local.foundation.pod_role_arns["aws-lb-controller"]

  depends_on = [module.eks]
}
