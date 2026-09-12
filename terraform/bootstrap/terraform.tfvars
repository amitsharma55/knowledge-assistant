aws_region  = "us-east-1"
name_prefix = "ka"

tags = {
  Project   = "knowledge-assistant"
  ManagedBy = "terraform"
  Stack     = "bootstrap"
}

state_noncurrent_version_days = 90
