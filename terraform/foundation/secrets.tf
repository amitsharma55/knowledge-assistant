# Terraform creates the secret but never its value: there is no secret_version
# resource here, so the key never enters state or git. The owner sets it with
# `aws secretsmanager put-secret-value`, and chat-api reads it at startup with
# its pod role.
resource "aws_secretsmanager_secret" "anthropic" {
  name                    = var.anthropic_secret_name
  description             = "Anthropic API key, read by chat-api at startup"
  recovery_window_in_days = var.secret_recovery_window_days

  lifecycle {
    prevent_destroy = true
  }
}

# ServiceNow integration-user password for the live ticket feature. Same
# value-stays-out-of-state pattern as the Anthropic key: chat-api reads it at
# startup via KA_SERVICENOW_SECRET_ID with its pod role. Store a plaintext
# password (the loader also accepts a single-key JSON object).
resource "aws_secretsmanager_secret" "servicenow" {
  name                    = var.servicenow_secret_name
  description             = "ServiceNow integration-user password, read by chat-api at startup"
  recovery_window_in_days = var.secret_recovery_window_days

  lifecycle {
    prevent_destroy = true
  }
}
