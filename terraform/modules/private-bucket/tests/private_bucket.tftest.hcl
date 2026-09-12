mock_provider "aws" {
  override_during = plan

  # aws_s3_bucket_policy.this.policy interpolates aws_s3_bucket.this.arn.
  # Without this override the bucket's arn stays "(known after apply)"
  # during a plan-only run, and the TLS-deny-policy assertion below
  # cannot evaluate. The override only fixes arn, which no assertion reads
  # directly, so the policy assertion stays meaningful.
  override_resource {
    target = aws_s3_bucket.this
    values = {
      arn = "arn:aws:s3:::zz-test-bucket"
    }
  }
}

variables {
  name                    = "zz-test-bucket"
  noncurrent_version_days = 7
}

run "applies_the_security_baseline" {
  command = plan

  assert {
    condition     = aws_s3_bucket.this.bucket == "zz-test-bucket"
    error_message = "The bucket name must come from var.name."
  }

  assert {
    condition     = one(aws_s3_bucket_versioning.this.versioning_configuration).status == "Enabled"
    error_message = "Versioning must be enabled."
  }

  assert {
    condition     = one(one(aws_s3_bucket_lifecycle_configuration.this.rule).noncurrent_version_expiration).noncurrent_days == 7
    error_message = "Noncurrent versions must expire after var.noncurrent_version_days."
  }

  assert {
    condition     = one(one(aws_s3_bucket_server_side_encryption_configuration.this.rule).apply_server_side_encryption_by_default).sse_algorithm == "AES256"
    error_message = "The bucket must use SSE-S3 encryption."
  }

  assert {
    condition = alltrue([
      aws_s3_bucket_public_access_block.this.block_public_acls,
      aws_s3_bucket_public_access_block.this.block_public_policy,
      aws_s3_bucket_public_access_block.this.ignore_public_acls,
      aws_s3_bucket_public_access_block.this.restrict_public_buckets,
    ])
    error_message = "All four public access blocks must be on."
  }

  assert {
    condition = (
      jsondecode(aws_s3_bucket_policy.this.policy).Statement[0].Effect == "Deny" &&
      jsondecode(aws_s3_bucket_policy.this.policy).Statement[0].Condition.Bool["aws:SecureTransport"] == "false"
    )
    error_message = "The bucket policy must deny requests not made over TLS."
  }
}
