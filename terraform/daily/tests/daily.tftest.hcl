# Unit tests for the daily stack, planned against a mocked AWS provider and a
# stubbed foundation remote state: no credentials, no account, no S3.
# Later tasks add resources and run blocks here.

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
    values = { partition = "aws" }
  }

  # The EKS module has its own caller-identity / session-context lookups. The
  # mock provider would feed them a random invalid ARN, so stub them here.
  override_data {
    target = module.eks.data.aws_caller_identity.current[0]
    values = {
      account_id = "123456789012"
      arn        = "arn:aws:iam::123456789012:user/tester"
      user_id    = "AIDATESTER"
    }
  }

  override_data {
    target = module.eks.data.aws_iam_session_context.current[0]
    values = {
      issuer_arn = "arn:aws:iam::123456789012:role/tester"
    }
  }

  # The KMS submodule builds a key policy from a policy document; the mock would
  # return non-JSON, so give the key a valid policy.
  override_data {
    target = module.eks.module.kms.data.aws_iam_policy_document.this[0]
    values = {
      json = "{}"
    }
  }

  # Same for our own OpenSearch access policy document.
  override_data {
    target = data.aws_iam_policy_document.opensearch
    values = {
      json = "{}"
    }
  }
}

# The foundation remote state, stubbed with fixture outputs. This is the whole
# 4a -> foundation contract; every field a later task reads must appear here.
override_data {
  target = data.terraform_remote_state.foundation
  values = {
    outputs = {
      aws_region                   = "us-east-1"
      vpc_id                       = "vpc-0abc"
      subnet_ids                   = ["subnet-0a", "subnet-0b"]
      opensearch_security_group_id = "sg-0os"
      eks_cluster_role_arn         = "arn:aws:iam::123456789012:role/zz-eks-cluster"
      eks_node_role_arn            = "arn:aws:iam::123456789012:role/zz-eks-node"
      admin_principal_arn          = "arn:aws:iam::123456789012:user/tester"
      docs_bucket                  = "zz-docs-123456789012"
      ecr_repository_urls = {
        "knowledge-assistant/chat-api"  = "123456789012.dkr.ecr.us-east-1.amazonaws.com/knowledge-assistant/chat-api"
        "knowledge-assistant/ingestion" = "123456789012.dkr.ecr.us-east-1.amazonaws.com/knowledge-assistant/ingestion"
        "knowledge-assistant/ui"        = "123456789012.dkr.ecr.us-east-1.amazonaws.com/knowledge-assistant/ui"
      }
      pod_role_arns = {
        "chat-api"          = "arn:aws:iam::123456789012:role/zz-chat-api"
        "ingestion"         = "arn:aws:iam::123456789012:role/zz-ingestion"
        "aws-lb-controller" = "arn:aws:iam::123456789012:role/zz-lb"
      }
      acm_certificate_arn = "arn:aws:acm:us-east-1:123456789012:certificate/zz-app-cert"
      app_hostname        = "app.example.test"
      route53_zone_id     = "Z0TEST0ZONE0ID"
    }
  }
}

variables {
  aws_region              = "us-east-1"
  allowed_account_ids     = ["123456789012"]
  name_prefix             = "zz"
  foundation_state_bucket = "zz-tfstate-123456789012"
  foundation_state_key    = "foundation/terraform.tfstate"
  tags                    = { Project = "p", ManagedBy = "m", Stack = "daily" }

  chat_api_namespace      = "knowledge-assistant"
  ingestion_namespace     = "knowledge-assistant"
  lb_controller_namespace = "kube-system"
}

run "foundation_wiring_resolves" {
  command = plan

  assert {
    condition     = local.foundation.vpc_id == "vpc-0abc"
    error_message = "foundation remote-state override did not resolve"
  }

  assert {
    condition     = local.foundation.pod_role_arns["chat-api"] == "arn:aws:iam::123456789012:role/zz-chat-api"
    error_message = "pod_role_arns not readable from foundation outputs"
  }
}

run "eks_wiring" {
  command = plan

  # Foundation owns identity: the module must create no cluster IAM role. Its
  # role-name/arn outputs are null when create_iam_role = false (bring-your-own).
  assert {
    condition     = module.eks.cluster_iam_role_name == null
    error_message = "cluster must reuse the foundation role, not create one (create_iam_role must be false)"
  }

  # Access-entry admin is the foundation admin principal.
  assert {
    condition     = aws_eks_access_entry.admin.principal_arn == "arn:aws:iam::123456789012:user/tester"
    error_message = "admin access entry must use the foundation admin_principal_arn"
  }

  # Cluster name derives from the prefix.
  assert {
    condition     = module.eks.cluster_name == "zz-daily"
    error_message = "cluster name must be <name_prefix>-daily"
  }
}

run "opensearch_is_single_node_vpc" {
  command = plan

  assert {
    condition     = aws_opensearch_domain.main.cluster_config[0].instance_count == 1
    error_message = "OpenSearch must be a single data node"
  }

  assert {
    condition     = aws_opensearch_domain.main.cluster_config[0].zone_awareness_enabled == false
    error_message = "OpenSearch must not be multi-AZ"
  }

  assert {
    condition     = aws_opensearch_domain.main.cluster_config[0].dedicated_master_enabled == false
    error_message = "OpenSearch must not have a dedicated master"
  }

  assert {
    condition     = contains(aws_opensearch_domain.main.vpc_options[0].security_group_ids, "sg-0os")
    error_message = "OpenSearch must attach the foundation security group"
  }

  assert {
    condition     = length(aws_opensearch_domain.main.vpc_options[0].subnet_ids) == 1
    error_message = "single-node OpenSearch must sit in exactly one subnet"
  }

  assert {
    condition     = aws_opensearch_domain.main.domain_endpoint_options[0].enforce_https == true
    error_message = "OpenSearch must enforce HTTPS"
  }

  assert {
    condition     = aws_opensearch_domain.main.encrypt_at_rest[0].enabled == true
    error_message = "OpenSearch must encrypt at rest"
  }
}

run "pod_identity_bindings" {
  command = plan

  assert {
    condition     = aws_eks_pod_identity_association.chat_api.role_arn == "arn:aws:iam::123456789012:role/zz-chat-api"
    error_message = "chat-api association must bind the foundation chat-api role"
  }

  assert {
    condition     = aws_eks_pod_identity_association.chat_api.service_account == "chat-api"
    error_message = "chat-api association must target the chat-api service account"
  }

  assert {
    condition     = aws_eks_pod_identity_association.ingestion.role_arn == "arn:aws:iam::123456789012:role/zz-ingestion"
    error_message = "ingestion association must bind the foundation ingestion role"
  }

  assert {
    condition     = aws_eks_pod_identity_association.lb_controller.namespace == "kube-system"
    error_message = "lb-controller association must target kube-system"
  }

  assert {
    condition     = aws_eks_pod_identity_association.lb_controller.role_arn == "arn:aws:iam::123456789012:role/zz-lb"
    error_message = "lb-controller association must bind the foundation aws-lb-controller role"
  }

  assert {
    condition     = aws_eks_pod_identity_association.chat_api.namespace == "knowledge-assistant"
    error_message = "chat-api association must bind the knowledge-assistant namespace"
  }

  assert {
    condition     = aws_eks_pod_identity_association.ingestion.namespace == "knowledge-assistant"
    error_message = "ingestion association must bind the knowledge-assistant namespace"
  }

  assert {
    condition     = output.docs_bucket != "" && output.vpc_id != ""
    error_message = "docs_bucket and vpc_id must be passed through from foundation"
  }

  assert {
    condition     = output.ecr_repository_urls["knowledge-assistant/chat-api"] != ""
    error_message = "ecr_repository_urls must expose the chat-api repository URL"
  }

  assert {
    condition     = output.app_hostname == "app.example.test" && output.acm_certificate_arn != "" && output.route53_zone_id != ""
    error_message = "TLS values (hostname, cert ARN, zone) must be passed through from foundation for deploy.sh"
  }
}
