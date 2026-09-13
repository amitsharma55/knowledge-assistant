output "state_bucket_name" {
  description = "Bucket for Terraform state; copy it into foundation/backend.hcl."
  value       = module.state_bucket.name
}

# The backend block cannot read variables, so foundation's backend.hcl needs the
# region from somewhere. Exposing it here lets `make tf-backend` derive the whole
# file from this stack rather than hard-coding a region a second time.
output "region" {
  description = "Region the state bucket lives in; foundation/backend.hcl reuses it."
  value       = var.aws_region
}
