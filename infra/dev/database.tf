# RDSはDB専用サブネットへ置く。DB内のアプリユーザーとテーブルは後で初期化する。
locals {
  db_identifier = "${var.name_prefix}-db"
}

resource "aws_db_subnet_group" "app" {
  name       = "${var.name_prefix}-db"
  subnet_ids = [for subnet in aws_subnet.db : subnet.id]
  tags       = { Name = "${var.name_prefix}-db" }
}

resource "aws_db_parameter_group" "app" {
  name   = "${var.name_prefix}-postgres18"
  family = "postgres18"

  parameter {
    name         = "rds.force_ssl"
    value        = "1"
    apply_method = "pending-reboot"
  }

  # SQL本文の常時・遅延・通常エラー時の記録を避ける。エラーメッセージ自体の内容は実機で確認する。
  parameter {
    name  = "log_statement"
    value = "none"
  }
  parameter {
    name  = "log_min_duration_statement"
    value = "-1"
  }
  parameter {
    name  = "log_min_error_statement"
    value = "panic"
  }

  tags = { Name = "${var.name_prefix}-postgres18" }
}

resource "aws_cloudwatch_log_group" "database" {
  name              = "/aws/rds/instance/${local.db_identifier}/postgresql"
  retention_in_days = 14
  skip_destroy      = false
  tags              = { Name = "${var.name_prefix}-postgresql" }
}

resource "aws_db_instance" "app" {
  identifier     = local.db_identifier
  engine         = "postgres"
  engine_version = "18.6"
  instance_class = "db.t4g.micro"
  db_name        = "readinglog"
  port           = 5432

  db_subnet_group_name   = aws_db_subnet_group.app.name
  vpc_security_group_ids = [aws_security_group.db.id]
  publicly_accessible    = false
  multi_az               = false
  parameter_group_name   = aws_db_parameter_group.app.name
  ca_cert_identifier     = "rds-ca-rsa2048-g1"

  storage_type          = "gp3"
  allocated_storage     = 20
  max_allocated_storage = 100
  storage_encrypted     = true

  # RDSが管理者パスワードを生成する。Terraformに値を渡さず、アプリにも使わせない。
  username                            = "readinglog_admin"
  manage_master_user_password         = true
  iam_database_authentication_enabled = false

  backup_retention_period   = 7
  backup_window             = "18:00-18:30"
  maintenance_window        = "sun:19:00-sun:20:00"
  copy_tags_to_snapshot     = true
  delete_automated_backups  = false
  deletion_protection       = var.db_deletion_protection
  skip_final_snapshot       = false
  final_snapshot_identifier = coalesce(var.db_final_snapshot_identifier, "${var.name_prefix}-final")

  # バージョン更新は復元検証後にコードで指定する。通常の変更は保守時間帯へ回す。
  auto_minor_version_upgrade  = false
  allow_major_version_upgrade = false
  apply_immediately           = false
  engine_lifecycle_support    = "open-source-rds-extended-support-disabled"

  enabled_cloudwatch_logs_exports = ["postgresql"]
  performance_insights_enabled    = false
  monitoring_interval             = 0
  tags                            = { Name = local.db_identifier }

  depends_on = [aws_cloudwatch_log_group.database]
}
