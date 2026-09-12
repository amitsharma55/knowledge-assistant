# Backend blocks cannot read variables, so every value comes from a gitignored
# backend.hcl: terraform init -backend-config=backend.hcl
terraform {
  backend "s3" {}
}
