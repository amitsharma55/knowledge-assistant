output "state_bucket_name" {
  description = "Bucket for Terraform state; copy it into foundation/backend.hcl."
  value       = module.state_bucket.name
}
