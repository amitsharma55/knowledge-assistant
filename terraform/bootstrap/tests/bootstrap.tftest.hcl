mock_provider "aws" {
  override_during = plan

  override_data {
    target = data.aws_caller_identity.current
    values = {
      account_id = "123456789012"
    }
  }
}

variables {
  aws_region                    = "us-east-1"
  allowed_account_ids           = ["123456789012"]
  name_prefix                   = "zz"
  tags                          = { Project = "p", ManagedBy = "m", Stack = "bootstrap" }
  state_noncurrent_version_days = 30
}

run "names_the_state_bucket_from_prefix_and_account" {
  command = plan

  assert {
    condition     = output.state_bucket_name == "zz-tfstate-123456789012"
    error_message = "The state bucket must be named <name_prefix>-tfstate-<account-id>."
  }
}

run "rejects_a_malformed_account_id" {
  command = plan

  variables {
    allowed_account_ids = ["not-an-account"]
  }

  expect_failures = [var.allowed_account_ids]
}

run "rejects_tags_without_a_stack" {
  command = plan

  variables {
    tags = { Project = "p", ManagedBy = "m" }
  }

  expect_failures = [var.tags]
}
