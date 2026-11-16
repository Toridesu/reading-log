# クラスターはタスクを管理する入れ物。実際のタスクとサービスはDB・認証の定義後に追加する。
resource "aws_ecs_cluster" "app" {
  name = var.name_prefix

  setting {
    name  = "containerInsights"
    value = "disabled"
  }

  tags = { Name = var.name_prefix }
}

# Spotの中断処理を初期範囲から外すため、通常のFargateを使う。
resource "aws_ecs_cluster_capacity_providers" "app" {
  cluster_name       = aws_ecs_cluster.app.name
  capacity_providers = ["FARGATE"]

  default_capacity_provider_strategy {
    capacity_provider = "FARGATE"
    weight            = 1
    base              = 0
  }
}

# ロググループはTerraformが先に作る。実行ロールへグループ作成の権限は渡さない。
resource "aws_cloudwatch_log_group" "app" {
  for_each = local.workloads

  name                        = "/ecs/${var.name_prefix}/${each.key}"
  retention_in_days           = 14
  log_group_class             = "STANDARD"
  deletion_protection_enabled = false
  skip_destroy                = false
  tags                        = { Name = "${var.name_prefix}-${each.key}", Workload = each.key }
}
