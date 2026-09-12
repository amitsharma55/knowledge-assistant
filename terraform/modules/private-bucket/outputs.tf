output "name" {
  description = "The bucket name."
  value       = aws_s3_bucket.this.bucket
}

output "arn" {
  description = "The bucket ARN."
  value       = aws_s3_bucket.this.arn
}
