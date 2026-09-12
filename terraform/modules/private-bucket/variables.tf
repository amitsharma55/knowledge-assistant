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
