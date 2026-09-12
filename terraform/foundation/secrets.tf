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
