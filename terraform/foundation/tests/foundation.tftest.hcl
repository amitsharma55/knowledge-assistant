# Unit tests for the foundation stack, planned against a mocked AWS provider:
# no credentials, no account. Later tasks add variables and run blocks here.

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
    values = {
      partition = "aws"
    }
  }

  override_data {
    target = data.aws_availability_zones.available
    values = {
      names    = ["zz-1a", "zz-1b", "zz-1c", "zz-1d"]
      zone_ids = ["zz1-az1", "zz1-az3", "zz1-az4", "zz1-az6"]
    }
  }

  # Fixed so IAM policy JSON that embeds these ARNs is known at plan time,
  # rather than unknown because the referenced resource is created in the
  # same plan.
  override_resource {
    target = aws_secretsmanager_secret.anthropic
    values = {
      arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:zz/anthropic-abc123"
    }
  }

  override_resource {
    target = module.docs_bucket.aws_s3_bucket.this
    values = {
      arn = "arn:aws:s3:::zz-docs-123456789012"
    }
  }
}

variables {
  # common
  aws_region          = "us-east-1"
  allowed_account_ids = ["123456789012"]
  name_prefix         = "zz"
  tags                = { Project = "p", ManagedBy = "m", Stack = "foundation" }

  # network
  vpc_cidr                 = "10.99.0.0/16"
  subnet_count             = 2
  subnet_newbits           = 4
  excluded_az_ids          = ["zz1-az3"]
  opensearch_ingress_cidrs = ["10.99.0.0/16"]

  # storage and secrets
  docs_noncurrent_version_days = 30
  ecr_repositories             = ["zz/one", "zz/two"]
  ecr_image_tag_mutability     = "MUTABLE"
  ecr_scan_on_push             = true
  ecr_keep_tagged_images       = 10
  ecr_untagged_expiry_days     = 1
  anthropic_secret_name        = "zz/anthropic"
  secret_recovery_window_days  = 7

  # iam
  chat_api_bedrock_model_ids  = ["zz.embed-v1", "zz.chat-v1"]
  ingestion_bedrock_model_ids = ["zz.embed-v1"]
  lb_controller_policy_file   = "policies/aws-lb-controller-v3.5.0.json"

  # budget
  budget_limit_usd               = 50
  budget_time_unit               = "MONTHLY"
  budget_actual_thresholds_pct   = [50, 80, 100]
  budget_forecast_thresholds_pct = [100]
  alert_emails                   = ["alerts@example.test"]
}

run "network_skips_excluded_zones" {
  command = plan

  assert {
    condition     = [for s in aws_subnet.public : s.availability_zone] == ["zz-1a", "zz-1c"]
    error_message = "Subnets must use the first zones whose IDs are not in excluded_az_ids."
  }

  assert {
    condition     = [for s in aws_subnet.public : s.cidr_block] == ["10.99.0.0/20", "10.99.16.0/20"]
    error_message = "Subnet CIDRs must be cidrsubnet(vpc_cidr, subnet_newbits, i)."
  }

  assert {
    condition     = alltrue([for s in aws_subnet.public : s.tags["kubernetes.io/role/elb"] == "1" && s.map_public_ip_on_launch])
    error_message = "Public subnets must be tagged for the load balancer controller and assign public IPs."
  }

  assert {
    condition     = aws_vpc.main.enable_dns_hostnames && aws_vpc.main.enable_dns_support
    error_message = "EKS and VPC OpenSearch domains need DNS support and DNS hostnames."
  }

  assert {
    condition     = one(aws_route_table.public.route).cidr_block == "0.0.0.0/0"
    error_message = "The public route table must send 0.0.0.0/0 to the internet gateway."
  }
}

run "opensearch_accepts_https_from_configured_cidrs_only" {
  command = plan

  assert {
    condition     = keys(aws_vpc_security_group_ingress_rule.opensearch_https) == ["10.99.0.0/16"]
    error_message = "OpenSearch ingress must come from opensearch_ingress_cidrs only."
  }

  assert {
    condition = alltrue([
      for r in aws_vpc_security_group_ingress_rule.opensearch_https :
      r.from_port == 443 && r.to_port == 443 && r.ip_protocol == "tcp"
    ])
    error_message = "OpenSearch ingress must be TCP 443."
  }
}

run "rejects_too_few_usable_zones" {
  command = plan

  variables {
    subnet_count = 4
  }

  expect_failures = [aws_vpc.main]
}

run "docs_bucket_is_named_from_prefix_and_account" {
  command = plan

  assert {
    condition     = module.docs_bucket.name == "zz-docs-123456789012"
    error_message = "The docs bucket must be named <name_prefix>-docs-<account-id>."
  }
}

run "ecr_repositories_scan_and_expire_old_images" {
  command = plan

  assert {
    condition     = keys(aws_ecr_repository.app) == ["zz/one", "zz/two"]
    error_message = "There must be one repository per ecr_repositories entry."
  }

  assert {
    condition = alltrue([
      for r in aws_ecr_repository.app :
      r.image_tag_mutability == "MUTABLE" && one(r.image_scanning_configuration).scan_on_push
    ])
    error_message = "Repositories must take tag mutability and scan-on-push from tfvars."
  }

  assert {
    condition = alltrue([
      for p in aws_ecr_lifecycle_policy.app :
      jsondecode(p.policy).rules[0].selection.tagStatus == "untagged" &&
      jsondecode(p.policy).rules[0].selection.countNumber == 1 &&
      jsondecode(p.policy).rules[1].selection.tagStatus == "tagged" &&
      jsondecode(p.policy).rules[1].selection.countNumber == 10
    ])
    error_message = "Lifecycle must expire untagged images after ecr_untagged_expiry_days and keep ecr_keep_tagged_images tagged ones."
  }
}

run "secret_is_created_with_its_recovery_window" {
  command = plan

  assert {
    condition     = aws_secretsmanager_secret.anthropic.name == "zz/anthropic"
    error_message = "The secret name must come from anthropic_secret_name."
  }

  assert {
    condition     = aws_secretsmanager_secret.anthropic.recovery_window_in_days == 7
    error_message = "The recovery window must come from secret_recovery_window_days."
  }
}

run "pod_roles_trust_pod_identity_not_a_cluster" {
  command = plan

  assert {
    condition = alltrue([
      for r in [aws_iam_role.chat_api, aws_iam_role.ingestion, aws_iam_role.aws_lb_controller] :
      jsondecode(r.assume_role_policy).Statement[0].Principal.Service == "pods.eks.amazonaws.com" &&
      toset(jsondecode(r.assume_role_policy).Statement[0].Action) == toset(["sts:AssumeRole", "sts:TagSession"])
    ])
    error_message = "Pod roles must trust pods.eks.amazonaws.com for AssumeRole and TagSession."
  }

  assert {
    condition     = aws_iam_role.chat_api.name == "zz-chat-api"
    error_message = "Role names must be built from name_prefix."
  }
}

run "chat_api_invokes_only_configured_models_and_reads_its_secret" {
  command = plan

  assert {
    condition = jsondecode(aws_iam_role_policy.chat_api.policy).Statement[0].Resource == [
      "arn:aws:bedrock:us-east-1::foundation-model/zz.embed-v1",
      "arn:aws:bedrock:us-east-1::foundation-model/zz.chat-v1",
    ]
    error_message = "chat-api must be limited to chat_api_bedrock_model_ids."
  }

  assert {
    condition     = jsondecode(aws_iam_role_policy.chat_api.policy).Statement[1].Resource == aws_secretsmanager_secret.anthropic.arn
    error_message = "chat-api must read only the Claude key secret."
  }

  assert {
    condition     = !strcontains(aws_iam_role_policy.chat_api.policy, "s3:")
    error_message = "chat-api must have no S3 access."
  }
}

run "ingestion_reads_docs_and_embeds_only" {
  command = plan

  assert {
    condition     = jsondecode(aws_iam_role_policy.ingestion.policy).Statement[0].Resource == module.docs_bucket.arn
    error_message = "ingestion must list only the docs bucket."
  }

  assert {
    condition     = jsondecode(aws_iam_role_policy.ingestion.policy).Statement[1].Resource == "${module.docs_bucket.arn}/*"
    error_message = "ingestion must read only objects in the docs bucket."
  }

  assert {
    condition     = jsondecode(aws_iam_role_policy.ingestion.policy).Statement[2].Resource == ["arn:aws:bedrock:us-east-1::foundation-model/zz.embed-v1"]
    error_message = "ingestion may invoke only ingestion_bedrock_model_ids."
  }

  assert {
    condition     = !strcontains(aws_iam_role_policy.ingestion.policy, "secretsmanager:")
    error_message = "ingestion must not read secrets."
  }
}

run "eks_roles_carry_their_managed_policies" {
  command = plan

  assert {
    condition     = aws_iam_role_policy_attachment.eks_cluster.policy_arn == "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
    error_message = "The cluster role needs AmazonEKSClusterPolicy."
  }

  assert {
    condition = toset([for a in aws_iam_role_policy_attachment.eks_node : a.policy_arn]) == toset([
      "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
      "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
      "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
    ])
    error_message = "The node role needs the worker, CNI and ECR read-only policies."
  }
}

run "lb_controller_policy_is_the_vendored_file" {
  command = plan

  assert {
    condition     = jsondecode(aws_iam_policy.aws_lb_controller.policy) == jsondecode(file("${path.module}/policies/aws-lb-controller-v3.5.0.json"))
    error_message = "The controller policy must be read from lb_controller_policy_file."
  }
}

run "opensearch_service_linked_role_is_created" {
  command = plan

  assert {
    condition     = aws_iam_service_linked_role.opensearch.aws_service_name == "opensearchservice.amazonaws.com"
    error_message = "The OpenSearch service-linked role must be created; VPC domains fail without it."
  }
}

run "budget_alerts_at_each_configured_threshold" {
  command = plan

  assert {
    condition     = aws_budgets_budget.monthly.name == "zz-monthly"
    error_message = "The budget name must be <name_prefix>-<time unit>."
  }

  assert {
    condition     = aws_budgets_budget.monthly.limit_amount == "50.0" && aws_budgets_budget.monthly.time_unit == "MONTHLY"
    error_message = "The limit and period must come from tfvars."
  }

  assert {
    condition = sort([
      for n in aws_budgets_budget.monthly.notification : "${n.notification_type}:${n.threshold}"
    ]) == sort(["ACTUAL:50", "ACTUAL:80", "ACTUAL:100", "FORECASTED:100"])
    error_message = "There must be one notification per actual and forecast threshold."
  }

  assert {
    condition     = alltrue([for n in aws_budgets_budget.monthly.notification : n.subscriber_email_addresses == toset(["alerts@example.test"])])
    error_message = "Every notification must go to alert_emails."
  }
}

run "rejects_an_invalid_alert_email" {
  command = plan

  variables {
    alert_emails = ["not-an-email"]
  }

  expect_failures = [var.alert_emails]
}

run "outputs_expose_what_the_daily_stack_and_verify_need" {
  command = plan

  assert {
    condition     = output.admin_principal_arn == "arn:aws:iam::123456789012:user/tester"
    error_message = "admin_principal_arn must be the identity applying the stack."
  }

  assert {
    condition     = keys(output.pod_role_arns) == ["aws-lb-controller", "chat-api", "ingestion"]
    error_message = "pod_role_arns must map every pod role."
  }

  assert {
    condition     = length(output.subnet_ids) == 2 && output.docs_bucket == "zz-docs-123456789012"
    error_message = "Network and docs outputs must be present."
  }

  assert {
    condition     = keys(output.ecr_repository_urls) == ["zz/one", "zz/two"]
    error_message = "ecr_repository_urls must map every repository."
  }

  assert {
    condition     = output.verify_expectations.budget_notification_count == 4 && output.verify_expectations.project_tag == "p"
    error_message = "verify_expectations must reflect the tfvars that verify.sh checks against."
  }
}
