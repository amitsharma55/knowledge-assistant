variable "name" {
  description = "Globally unique bucket name."
  type        = string
}

variable "noncurrent_version_days" {
  description = "Days to keep noncurrent object versions before they expire."
  type        = number

  validation {
    condition     = var.noncurrent_version_days >= 1
    error_message = "noncurrent_version_days must be at least 1."
  }
}

variable "object_expiration_days" {
  description = <<-EOT
    Days after which current objects are deleted. 0 (the default) means objects
    are kept indefinitely, which is correct for durable stores. Set a positive
    value for a bucket holding data with a retention limit -- e.g. the query log,
    whose objects are user queries (PII) and must not live forever.
  EOT
  type        = number
  default     = 0

  validation {
    condition     = var.object_expiration_days >= 0
    error_message = "object_expiration_days must be 0 (keep forever) or a positive number of days."
  }
}
