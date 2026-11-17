# RDSの配置・TLS・バックアップと、管理者Secretをアプリへ渡さないことを確認する。
mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_resource "aws_db_instance" {
    defaults = {
      master_user_secret = [{
        secret_arn    = "arn:aws:secretsmanager:ap-northeast-1:123456789012:secret:rds!db-test-ABCDEF"
        secret_status = "active"
        kms_key_id    = "arn:aws:kms:ap-northeast-1:123456789012:key/00000000-0000-0000-0000-000000000001"
      }]
    }
  }
}

override_resource {
  target = aws_secretsmanager_secret.database_url
  values = { arn = "arn:aws:secretsmanager:ap-northeast-1:123456789012:secret:reading-log-dev/database-url-UVWXYZ" }
}

run "private_database" {
  command = apply

  assert {
    condition = (
      toset(aws_db_subnet_group.app.subnet_ids) == toset([for subnet in aws_subnet.db : subnet.id]) &&
      aws_db_instance.app.db_subnet_group_name == aws_db_subnet_group.app.name &&
      toset(aws_db_instance.app.vpc_security_group_ids) == toset([aws_security_group.db.id]) &&
      !aws_db_instance.app.publicly_accessible && !aws_db_instance.app.multi_az && aws_db_instance.app.port == 5432
    )
    error_message = "RDSをDB専用サブネット・SGへ接続し、プライベートなSingle-AZ/5432番にしてください。"
  }
  assert {
    condition = (
      aws_db_instance.app.engine == "postgres" && aws_db_instance.app.engine_version == "18.6" &&
      aws_db_instance.app.instance_class == "db.t4g.micro" && aws_db_instance.app.storage_encrypted &&
      aws_db_instance.app.storage_type == "gp3" && aws_db_instance.app.allocated_storage == 20 &&
      aws_db_instance.app.max_allocated_storage == 100 &&
      aws_db_instance.app.engine_lifecycle_support == "open-source-rds-extended-support-disabled"
    )
    error_message = "PostgreSQLの版・暗号化・容量と、自動延長サポートを使わない方針を確認してください。"
  }
  assert {
    condition = (
      aws_db_parameter_group.app.family == "postgres18" &&
      aws_db_instance.app.parameter_group_name == aws_db_parameter_group.app.name &&
      aws_db_instance.app.ca_cert_identifier == "rds-ca-rsa2048-g1" &&
      one([for parameter in aws_db_parameter_group.app.parameter : parameter.value if parameter.name == "rds.force_ssl"]) == "1"
    )
    error_message = "PostgreSQL18用のTLS必須パラメーターとRDSのCAを設定してください。"
  }
  assert {
    condition = (
      aws_cloudwatch_log_group.database.name == "/aws/rds/instance/${aws_db_instance.app.identifier}/postgresql" &&
      aws_cloudwatch_log_group.database.retention_in_days == 14 &&
      aws_db_instance.app.enabled_cloudwatch_logs_exports == toset(["postgresql"]) &&
      one([for parameter in aws_db_parameter_group.app.parameter : parameter.value if parameter.name == "log_statement"]) == "none" &&
      one([for parameter in aws_db_parameter_group.app.parameter : parameter.value if parameter.name == "log_min_duration_statement"]) == "-1" &&
      one([for parameter in aws_db_parameter_group.app.parameter : parameter.value if parameter.name == "log_min_error_statement"]) == "panic"
    )
    error_message = "DBログを14日保持し、通常のSQL本文を記録する設定を無効にしてください。"
  }
}

run "backup_and_delete_defaults" {
  command = plan

  assert {
    condition = (
      aws_db_instance.app.backup_retention_period == 7 && aws_db_instance.app.copy_tags_to_snapshot &&
      !aws_db_instance.app.delete_automated_backups && aws_db_instance.app.deletion_protection &&
      !aws_db_instance.app.skip_final_snapshot && aws_db_instance.app.final_snapshot_identifier == "reading-log-dev-final"
    )
    error_message = "7日バックアップ、削除保護、最終スナップショットと削除後の自動バックアップ保持を設定してください。"
  }
  assert {
    condition = (
      !aws_db_instance.app.apply_immediately && !aws_db_instance.app.auto_minor_version_upgrade &&
      !aws_db_instance.app.allow_major_version_upgrade &&
      aws_db_instance.app.backup_window == "18:00-18:30" && aws_db_instance.app.maintenance_window == "sun:19:00-sun:20:00"
    )
    error_message = "バージョン更新をコードで管理し、バックアップと保守時間帯を分けてください。"
  }
}

run "application_secret_boundary" {
  command = apply

  assert {
    condition = (
      aws_db_instance.app.manage_master_user_password && aws_db_instance.app.password == null &&
      aws_db_instance.app.username == "readinglog_admin" &&
      aws_secretsmanager_secret.database_url.recovery_window_in_days == 7 &&
      aws_secretsmanager_secret.database_url.name == "${var.name_prefix}/database-url" &&
      output.database.application_user == "readinglog"
    )
    error_message = "管理者パスワードはRDSへ委ね、アプリの接続情報を別Secretに分けてください。"
  }
  assert {
    condition = toset(keys(aws_iam_role_policy.database_secret)) == toset(["web", "migrate"]) && alltrue([
      for workload, policy in aws_iam_role_policy.database_secret :
      policy.role == aws_iam_role.execution[workload].name && policy.role != aws_iam_role.task[workload].name &&
      length(jsondecode(policy.policy).Statement) == 1 &&
      jsondecode(policy.policy).Statement[0].Effect == "Allow" &&
      toset(jsondecode(policy.policy).Statement[0].Action) == toset(["secretsmanager:GetSecretValue"]) &&
      jsondecode(policy.policy).Statement[0].Resource == aws_secretsmanager_secret.database_url.arn &&
      jsondecode(policy.policy).Statement[0].Resource != aws_db_instance.app.master_user_secret[0].secret_arn
    ])
    error_message = "Web/migrateの実行ロールにはアプリ用Secretの取得だけを許可し、管理者Secretや書き込みを許可しないでください。"
  }
}

run "prepare_intentional_delete" {
  command = plan
  variables {
    db_deletion_protection       = false
    db_final_snapshot_identifier = "reading-log-dev-final-20261116"
  }
  assert {
    condition     = !aws_db_instance.app.deletion_protection && !aws_db_instance.app.skip_final_snapshot && aws_db_instance.app.final_snapshot_identifier == "reading-log-dev-final-20261116"
    error_message = "削除保護解除後も最終スナップショットを残し、指定した固有名を使ってください。"
  }
}

run "reject_invalid_snapshot_name" {
  command = plan
  variables {
    db_final_snapshot_identifier = "invalid--snapshot"
  }
  expect_failures = [var.db_final_snapshot_identifier]
}

run "reject_invalid_database_prefix" {
  command = plan
  variables {
    name_prefix = "reading-log-"
  }
  expect_failures = [var.name_prefix]
}

run "bootstrap_secret_permissions" {
  command = apply
  assert {
    condition = (
      aws_iam_role_policy.bootstrap_secrets.role == aws_iam_role.task["bootstrap"].name &&
      length(jsondecode(aws_iam_role_policy.bootstrap_secrets.policy).Statement) == 2 &&
      jsondecode(aws_iam_role_policy.bootstrap_secrets.policy).Statement[0].Effect == "Allow" &&
      toset(jsondecode(aws_iam_role_policy.bootstrap_secrets.policy).Statement[0].Action) == toset(["secretsmanager:GetSecretValue"]) &&
      jsondecode(aws_iam_role_policy.bootstrap_secrets.policy).Statement[0].Resource == aws_db_instance.app.master_user_secret[0].secret_arn &&
      jsondecode(aws_iam_role_policy.bootstrap_secrets.policy).Statement[1].Effect == "Allow" &&
      toset(jsondecode(aws_iam_role_policy.bootstrap_secrets.policy).Statement[1].Action) == toset(["secretsmanager:GetSecretValue", "secretsmanager:PutSecretValue"]) &&
      jsondecode(aws_iam_role_policy.bootstrap_secrets.policy).Statement[1].Resource == aws_secretsmanager_secret.database_url.arn &&
      !contains(keys(aws_iam_role_policy.database_secret), "bootstrap")
    )
    error_message = "初期化のタスクロールだけに管理者Secretの読み取りとアプリSecretの読み書きを許可してください。"
  }
}
