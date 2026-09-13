aws_region           = "us-east-1"
name_prefix          = "ka"
foundation_state_key = "foundation/terraform.tfstate"

tags = {
  Project   = "knowledge-assistant"
  ManagedBy = "terraform"
  Stack     = "daily"
}

kubernetes_version  = "1.31"
node_instance_types = ["t3.medium"]
node_min_size       = 1
node_desired_size   = 1
node_max_size       = 2

opensearch_engine_version = "OpenSearch_2.13"
opensearch_instance_type  = "t3.small.search"
opensearch_volume_size    = 20
opensearch_tls_policy     = "Policy-Min-TLS-1-2-2019-07"

chat_api_namespace      = "knowledge-assistant"
ingestion_namespace     = "knowledge-assistant"
lb_controller_namespace = "kube-system"
