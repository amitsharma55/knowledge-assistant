# Documents live under one prefix per team (coupa/, star/, hr/); the ingestion
# job reads them from here every morning to refill the day's empty index.
module "docs_bucket" {
  source = "../modules/private-bucket"

  name                    = "${var.name_prefix}-docs-${data.aws_caller_identity.current.account_id}"
  noncurrent_version_days = var.docs_noncurrent_version_days
}

# Not force-deleted: destroying a repository that still holds images fails
# rather than losing them.
resource "aws_ecr_repository" "app" {
  for_each = var.ecr_repositories

  name                 = each.value
  image_tag_mutability = var.ecr_image_tag_mutability

  image_scanning_configuration {
    scan_on_push = var.ecr_scan_on_push
  }
}

resource "aws_ecr_lifecycle_policy" "app" {
  for_each = aws_ecr_repository.app

  repository = each.value.name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Expire untagged images"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = var.ecr_untagged_expiry_days
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "Keep only the most recent tagged images"
        selection = {
          tagStatus      = "tagged"
          tagPatternList = ["*"]
          countType      = "imageCountMoreThan"
          countNumber    = var.ecr_keep_tagged_images
        }
        action = { type = "expire" }
      },
    ]
  })
}
