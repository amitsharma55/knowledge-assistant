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
