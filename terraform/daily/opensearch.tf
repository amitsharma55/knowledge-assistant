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
