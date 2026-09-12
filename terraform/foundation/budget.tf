# Account-wide on purpose: other spend in the account is small, and orphaned
# resources (a load balancer left behind by a failed teardown) may not carry
# project tags, so a tag-filtered budget would miss exactly the costs it exists
# to catch.
resource "aws_budgets_budget" "monthly" {
  name        = "${var.name_prefix}-${lower(var.budget_time_unit)}"
  budget_type = "COST"
  # One decimal place, the form the Budgets API returns, so plans stay clean.
  limit_amount = format("%.1f", var.budget_limit_usd)
  limit_unit   = "USD"
  time_unit    = var.budget_time_unit

  dynamic "notification" {
    for_each = var.budget_actual_thresholds_pct

    content {
      comparison_operator        = "GREATER_THAN"
      threshold                  = notification.value
      threshold_type             = "PERCENTAGE"
      notification_type          = "ACTUAL"
      subscriber_email_addresses = var.alert_emails
    }
  }

  dynamic "notification" {
    for_each = var.budget_forecast_thresholds_pct

    content {
      comparison_operator        = "GREATER_THAN"
      threshold                  = notification.value
      threshold_type             = "PERCENTAGE"
      notification_type          = "FORECASTED"
      subscriber_email_addresses = var.alert_emails
    }
  }
}
