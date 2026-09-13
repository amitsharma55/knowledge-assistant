data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

# The foundation stack's outputs. Its state key is fixed; the bucket carries the
# account ID, so it comes from the gitignored private.auto.tfvars.
data "terraform_remote_state" "foundation" {
  backend = "s3"

  config = {
    bucket = var.foundation_state_bucket
    key    = var.foundation_state_key
    region = var.aws_region
  }
}

locals {
  foundation = data.terraform_remote_state.foundation.outputs
}
