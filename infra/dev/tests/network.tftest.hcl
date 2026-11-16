# AWS APIの代わりにmockを使う。applyはテスト内のシミュレーションで、実リソースを作らない。
mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
}

# SG同士を取り違えた場合も検出できるよう、異なるIDを割り当てる。
override_resource {
  target = aws_security_group.alb
  values = { id = "sg-11111111111111111" }
}
override_resource {
  target = aws_security_group.app
  values = { id = "sg-22222222222222222" }
}
override_resource {
  target = aws_security_group.db
  values = { id = "sg-33333333333333333" }
}

run "network_paths" {
  command = apply

  assert {
    condition     = length(aws_subnet.public) == 2 && length(aws_subnet.app) == 2 && length(aws_subnet.db) == 2
    error_message = "公開・アプリ・DBの各層を2AZへ配置してください。"
  }
  assert {
    condition = length(toset(concat(output.subnet_cidrs.public, output.subnet_cidrs.app, output.subnet_cidrs.db))) == 6 && alltrue([
      for cidr in concat(output.subnet_cidrs.public, output.subnet_cidrs.app, output.subnet_cidrs.db) : endswith(cidr, "/24")
    ])
    error_message = "6つのサブネットは重複しない/24にしてください。"
  }
  assert {
    condition = alltrue([
      for key, subnet in aws_subnet.app : !subnet.map_public_ip_on_launch && subnet.availability_zone == var.availability_zones[tonumber(key)]
      ]) && alltrue([
      for key, subnet in aws_subnet.db : !subnet.map_public_ip_on_launch && subnet.availability_zone == var.availability_zones[tonumber(key)]
    ])
    error_message = "アプリ・DBのAZ割り当てとパブリックIP無効を確認してください。"
  }
  assert {
    condition     = aws_route.public_internet.gateway_id == aws_internet_gateway.main.id && aws_route.public_internet.route_table_id == aws_route_table.public.id && aws_route.public_internet.destination_cidr_block == "0.0.0.0/0"
    error_message = "公開サブネットのデフォルト経路はIGWへ向けてください。"
  }
  assert {
    condition = aws_nat_gateway.main.subnet_id == aws_subnet.public["0"].id && aws_nat_gateway.main.connectivity_type == "public" && alltrue([
      for key, route in aws_route.app_internet : route.route_table_id == aws_route_table.app[key].id && route.nat_gateway_id == aws_nat_gateway.main.id && route.destination_cidr_block == "0.0.0.0/0"
    ])
    error_message = "アプリ2AZの外向き経路は、先頭AZの公開NATへ向けてください。"
  }
  assert {
    condition = alltrue([
      for key, association in aws_route_table_association.public : association.subnet_id == aws_subnet.public[key].id && association.route_table_id == aws_route_table.public.id
      ]) && alltrue([
      for key, association in aws_route_table_association.app : association.subnet_id == aws_subnet.app[key].id && association.route_table_id == aws_route_table.app[key].id
      ]) && alltrue([
      for key, association in aws_route_table_association.db : association.subnet_id == aws_subnet.db[key].id && association.route_table_id == aws_route_table.db[key].id && length(aws_route_table.db[key].route) == 0
    ])
    error_message = "サブネットとルート表を層ごとに関連付け、DBには外向きルートを作らないでください。"
  }
}

run "security_boundaries" {
  command = apply

  assert {
    condition     = aws_vpc_security_group_ingress_rule.alb_https.cidr_ipv4 == "0.0.0.0/0" && aws_vpc_security_group_ingress_rule.alb_https.security_group_id == aws_security_group.alb.id && aws_vpc_security_group_ingress_rule.alb_https.ip_protocol == "tcp" && aws_vpc_security_group_ingress_rule.alb_https.from_port == 443 && aws_vpc_security_group_ingress_rule.alb_https.to_port == 443
    error_message = "インターネットから受け付ける入口はALBのHTTPSにしてください。"
  }
  assert {
    condition     = aws_vpc_security_group_ingress_rule.app_alb.security_group_id == aws_security_group.app.id && aws_vpc_security_group_ingress_rule.app_alb.referenced_security_group_id == aws_security_group.alb.id && aws_vpc_security_group_ingress_rule.app_alb.cidr_ipv4 == null && aws_vpc_security_group_ingress_rule.app_alb.ip_protocol == "tcp" && aws_vpc_security_group_ingress_rule.app_alb.from_port == 8000 && aws_vpc_security_group_ingress_rule.app_alb.to_port == 8000
    error_message = "アプリ8000番の接続元はALBのSGだけにしてください。"
  }
  assert {
    condition     = aws_vpc_security_group_egress_rule.alb_app.security_group_id == aws_security_group.alb.id && aws_vpc_security_group_egress_rule.alb_app.referenced_security_group_id == aws_security_group.app.id && aws_vpc_security_group_egress_rule.alb_app.from_port == 8000 && aws_vpc_security_group_egress_rule.alb_app.to_port == 8000 && aws_vpc_security_group_egress_rule.alb_app.ip_protocol == "tcp"
    error_message = "ALBからアプリへ必要なポートだけを許可してください。"
  }
  assert {
    condition     = aws_vpc_security_group_ingress_rule.db_app.security_group_id == aws_security_group.db.id && aws_vpc_security_group_ingress_rule.db_app.referenced_security_group_id == aws_security_group.app.id && aws_vpc_security_group_ingress_rule.db_app.cidr_ipv4 == null && aws_vpc_security_group_ingress_rule.db_app.from_port == 5432 && aws_vpc_security_group_ingress_rule.db_app.to_port == 5432 && aws_vpc_security_group_ingress_rule.db_app.ip_protocol == "tcp"
    error_message = "DB5432番の接続元はアプリのSGだけにしてください。"
  }
  assert {
    condition     = aws_vpc_security_group_egress_rule.app_db.security_group_id == aws_security_group.app.id && aws_vpc_security_group_egress_rule.app_db.referenced_security_group_id == aws_security_group.db.id && aws_vpc_security_group_egress_rule.app_db.from_port == 5432 && aws_vpc_security_group_egress_rule.app_db.to_port == 5432 && aws_vpc_security_group_egress_rule.app_db.ip_protocol == "tcp"
    error_message = "アプリからDBへの出口をPostgreSQLだけにしてください。"
  }
  assert {
    condition     = aws_vpc_security_group_egress_rule.app_https.security_group_id == aws_security_group.app.id && aws_vpc_security_group_egress_rule.app_https.cidr_ipv4 == "0.0.0.0/0" && aws_vpc_security_group_egress_rule.app_https.from_port == 443 && aws_vpc_security_group_egress_rule.app_https.to_port == 443 && aws_vpc_security_group_egress_rule.app_https.ip_protocol == "tcp"
    error_message = "アプリのインターネット向け通信はHTTPSだけにしてください。"
  }
  assert {
    condition     = length(aws_default_security_group.main.ingress) == 0 && length(aws_default_security_group.main.egress) == 0
    error_message = "未使用のデフォルトSGは閉じてください。"
  }
}

run "s3_image_layers_only" {
  command = apply

  assert {
    condition     = aws_vpc_endpoint.s3.vpc_endpoint_type == "Gateway" && aws_vpc_endpoint.s3.service_name == "com.amazonaws.ap-northeast-1.s3" && toset(aws_vpc_endpoint.s3.route_table_ids) == toset([for table in aws_route_table.app : table.id])
    error_message = "S3 Gateway Endpointをアプリのルート表だけに関連付けてください。"
  }
  assert {
    condition     = length(jsondecode(aws_vpc_endpoint.s3.policy).Statement) == 1 && jsondecode(aws_vpc_endpoint.s3.policy).Statement[0].Effect == "Allow" && jsondecode(aws_vpc_endpoint.s3.policy).Statement[0].Action == "s3:GetObject" && jsondecode(aws_vpc_endpoint.s3.policy).Statement[0].Resource == "arn:aws:s3:::prod-ap-northeast-1-starport-layer-bucket/*"
    error_message = "S3経路は東京のECRイメージレイヤーの読み取りだけにしてください。"
  }
}

run "alternate_private_cidr" {
  command = plan
  variables {
    vpc_cidr = "10.30.0.0/16"
  }
  assert {
    condition     = alltrue([for cidr in concat(output.subnet_cidrs.public, output.subnet_cidrs.app, output.subnet_cidrs.db) : startswith(cidr, "10.30.")])
    error_message = "VPCのCIDR変更を全層のサブネットへ反映してください。"
  }
}

run "reject_public_cidr" {
  command = plan
  variables {
    vpc_cidr = "203.0.113.0/24"
  }
  expect_failures = [var.vpc_cidr]
}

run "reject_duplicate_az" {
  command = plan
  variables {
    availability_zones = ["ap-northeast-1a", "ap-northeast-1a"]
  }
  expect_failures = [var.availability_zones]
}

run "reject_other_region_az" {
  command = plan
  variables {
    availability_zones = ["us-east-1a", "us-east-1b"]
  }
  expect_failures = [var.availability_zones]
}

run "reject_single_az" {
  command = plan
  variables {
    availability_zones = ["ap-northeast-1a"]
  }
  expect_failures = [var.availability_zones]
}
