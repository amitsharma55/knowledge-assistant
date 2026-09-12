# The one bucket every other stack keeps its state in. This stack's own state
# stays local (and gitignored): losing it is harmless, since the bucket can be
# imported back.

data "aws_caller_identity" "current" {}

module "state_bucket" {
  source = "../modules/private-bucket"

  # The account ID makes the name globally unique without committing it.
  name                    = "${var.name_prefix}-tfstate-${data.aws_caller_identity.current.account_id}"
  noncurrent_version_days = var.state_noncurrent_version_days
}
