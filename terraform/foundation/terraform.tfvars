# ---- common ----
aws_region  = "us-east-1"
name_prefix = "ka"

tags = {
  Project   = "knowledge-assistant"
  ManagedBy = "terraform"
  Stack     = "foundation"
}

# ---- network ----
vpc_cidr       = "10.40.0.0/16"
subnet_count   = 2
subnet_newbits = 4
# use1-az3 cannot host an EKS control plane.
excluded_az_ids          = ["use1-az3"]
opensearch_ingress_cidrs = ["10.40.0.0/16"]

# ---- storage and secrets ----
docs_noncurrent_version_days = 30

# The image names deploy/k8s/*.yaml already use.
ecr_repositories = [
  "knowledge-assistant/chat-api",
  "knowledge-assistant/ingestion",
  "knowledge-assistant/ui",
]
ecr_image_tag_mutability = "MUTABLE"
ecr_scan_on_push         = true
ecr_keep_tagged_images   = 10
ecr_untagged_expiry_days = 1

anthropic_secret_name       = "ka/anthropic-api-key"
secret_recovery_window_days = 7

# ---- iam ----
# Titan v2 for embeddings; gpt-oss-20b for rerank and query rewrite.
chat_api_bedrock_model_ids = [
  "amazon.titan-embed-text-v2:0",
  "openai.gpt-oss-20b-1:0",
]
ingestion_bedrock_model_ids = ["amazon.titan-embed-text-v2:0"]
lb_controller_policy_file   = "policies/aws-lb-controller-v3.5.0.json"

# ---- budget ----
# Account-wide; the expected daily-environment spend is about $60/month, so
# the 100% alert is a deliberate monthly prompt.
budget_limit_usd               = 50
budget_time_unit               = "MONTHLY"
budget_actual_thresholds_pct   = [50, 80, 100]
budget_forecast_thresholds_pct = [100]
