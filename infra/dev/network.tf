# 公開用・アプリ用・DB用を2AZへ配置する。DBのルート表には外向き経路を追加しない。
locals {
  zones = { for index, az in var.availability_zones : tostring(index) => az }
}

resource "aws_vpc" "main" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = var.name_prefix }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${var.name_prefix}-igw" }
}

resource "aws_subnet" "public" {
  for_each = local.zones

  vpc_id                  = aws_vpc.main.id
  availability_zone       = each.value
  cidr_block              = cidrsubnet(var.vpc_cidr, 8, tonumber(each.key))
  map_public_ip_on_launch = false
  tags                    = { Name = "${var.name_prefix}-public-${each.key}", Tier = "public" }
}

resource "aws_subnet" "app" {
  for_each = local.zones

  vpc_id                  = aws_vpc.main.id
  availability_zone       = each.value
  cidr_block              = cidrsubnet(var.vpc_cidr, 8, 16 + tonumber(each.key))
  map_public_ip_on_launch = false
  tags                    = { Name = "${var.name_prefix}-app-${each.key}", Tier = "app" }
}

resource "aws_subnet" "db" {
  for_each = local.zones

  vpc_id                  = aws_vpc.main.id
  availability_zone       = each.value
  cidr_block              = cidrsubnet(var.vpc_cidr, 8, 32 + tonumber(each.key))
  map_public_ip_on_launch = false
  tags                    = { Name = "${var.name_prefix}-db-${each.key}", Tier = "db" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${var.name_prefix}-public" }
}

resource "aws_route" "public_internet" {
  route_table_id         = aws_route_table.public.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.main.id
}

resource "aws_route_table_association" "public" {
  for_each = local.zones

  subnet_id      = aws_subnet.public[each.key].id
  route_table_id = aws_route_table.public.id
}

resource "aws_eip" "nat" {
  domain = "vpc"
  tags   = { Name = "${var.name_prefix}-nat" }
}

# 最小版はNATを1AZに置く。別AZもこのNATを使うため、NAT側AZの停止に依存する。
resource "aws_nat_gateway" "main" {
  allocation_id     = aws_eip.nat.id
  subnet_id         = aws_subnet.public["0"].id
  connectivity_type = "public"
  tags              = { Name = "${var.name_prefix}-nat" }
  depends_on        = [aws_internet_gateway.main]
}

resource "aws_route_table" "app" {
  for_each = local.zones

  vpc_id = aws_vpc.main.id
  tags   = { Name = "${var.name_prefix}-app-${each.key}" }
}

resource "aws_route" "app_internet" {
  for_each = local.zones

  route_table_id         = aws_route_table.app[each.key].id
  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.main.id
}

resource "aws_route_table_association" "app" {
  for_each = local.zones

  subnet_id      = aws_subnet.app[each.key].id
  route_table_id = aws_route_table.app[each.key].id
}

resource "aws_route_table" "db" {
  for_each = local.zones

  # VPC内のlocalルート以外は持たせない。
  route  = []
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${var.name_prefix}-db-${each.key}" }
}

resource "aws_route_table_association" "db" {
  for_each = local.zones

  subnet_id      = aws_subnet.db[each.key].id
  route_table_id = aws_route_table.db[each.key].id
}

# ECRのイメージレイヤーだけをS3経由で取得する。DB用ルート表には関連付けない。
resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.main.id
  service_name      = "com.amazonaws.ap-northeast-1.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [for table in aws_route_table.app : table.id]
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "ReadECRImageLayers"
      Effect    = "Allow"
      Principal = "*"
      Action    = "s3:GetObject"
      Resource  = "arn:aws:s3:::prod-ap-northeast-1-starport-layer-bucket/*"
    }]
  })
  tags = { Name = "${var.name_prefix}-s3" }
}
