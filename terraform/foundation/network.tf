locals {
  # EKS cannot place a control plane in some zones, so zones are filtered by
  # zone ID (stable across accounts, unlike zone names) before the first
  # subnet_count are used.
  usable_azs = [
    for i, name in data.aws_availability_zones.available.names : name
    if !contains(var.excluded_az_ids, data.aws_availability_zones.available.zone_ids[i])
  ]
  subnet_azs = slice(local.usable_azs, 0, min(var.subnet_count, length(local.usable_azs)))
}

resource "aws_vpc" "main" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = {
    Name = "${var.name_prefix}-vpc"
  }

  lifecycle {
    precondition {
      condition     = length(local.usable_azs) >= var.subnet_count
      error_message = "Too few availability zones remain after excluded_az_ids for subnet_count subnets."
    }
  }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name = "${var.name_prefix}-igw"
  }
}

# Public subnets only: there is no NAT gateway, so nodes need public IPs to
# reach ECR and Bedrock.
resource "aws_subnet" "public" {
  count = length(local.subnet_azs)

  vpc_id                  = aws_vpc.main.id
  availability_zone       = local.subnet_azs[count.index]
  cidr_block              = cidrsubnet(var.vpc_cidr, var.subnet_newbits, count.index)
  map_public_ip_on_launch = true

  tags = {
    Name                     = "${var.name_prefix}-public-${local.subnet_azs[count.index]}"
    "kubernetes.io/role/elb" = "1"
  }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  tags = {
    Name = "${var.name_prefix}-public"
  }
}

resource "aws_route_table_association" "public" {
  count = length(aws_subnet.public)

  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

# The daily OpenSearch domain is reachable only from inside the VPC, which is
# why chat-api can keep using an unsigned client. Terraform removes the default
# allow-all egress rule; the domain never initiates connections.
resource "aws_security_group" "opensearch" {
  name        = "${var.name_prefix}-opensearch"
  description = "HTTPS to the OpenSearch domain from inside the VPC only"
  vpc_id      = aws_vpc.main.id

  tags = {
    Name = "${var.name_prefix}-opensearch"
  }
}

resource "aws_vpc_security_group_ingress_rule" "opensearch_https" {
  for_each = toset(var.opensearch_ingress_cidrs)

  security_group_id = aws_security_group.opensearch.id
  description       = "HTTPS from ${each.value}"
  cidr_ipv4         = each.value
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
}
