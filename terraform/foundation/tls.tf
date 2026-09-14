# The domain's public hosted zone is created by the Route 53 registrar at
# purchase, not by Terraform, so it is read here, never managed. deploy.sh
# upserts the app record into this zone each daily cycle.
data "aws_route53_zone" "app" {
  name         = var.domain_name
  private_zone = false
}

# A DNS-validated public certificate for the app hostname. The daily ALB
# references it by ARN; ACM auto-renews as long as the validation record below
# stays in the zone. Persisting the cert here (not in the disposable daily
# stack) avoids re-validating on every bring-up.
resource "aws_acm_certificate" "app" {
  domain_name       = var.app_hostname
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

# The CNAME ACM checks to prove control of the name. for_each over the
# validation options handles it generically; a single-name cert yields one.
resource "aws_route53_record" "app_cert_validation" {
  for_each = {
    for dvo in aws_acm_certificate.app.domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      type   = dvo.resource_record_type
      record = dvo.resource_record_value
    }
  }

  zone_id         = data.aws_route53_zone.app.zone_id
  name            = each.value.name
  type            = each.value.type
  records         = [each.value.record]
  ttl             = 60
  allow_overwrite = true
}

# Blocks until the certificate is issued, so consumers never get an ARN that
# cannot yet terminate TLS.
resource "aws_acm_certificate_validation" "app" {
  certificate_arn         = aws_acm_certificate.app.arn
  validation_record_fqdns = [for r in aws_route53_record.app_cert_validation : r.fqdn]
}
