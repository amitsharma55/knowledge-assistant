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
    vpc-cni = {
      before_compute = true
    }
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

  policy_arn = "arn:${data.aws_partition.current.partition}:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy"

  access_scope {
    type = "cluster"
  }

  depends_on = [aws_eks_access_entry.admin]
}

locals {
  cluster_name = module.eks.cluster_name
}
