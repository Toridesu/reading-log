# 接続URLの保存先だけを作る。実際の値はDBユーザー初期化後に投入し、Terraformで読み戻さない。
resource "aws_secretsmanager_secret" "database_url" {
  name                    = "${var.name_prefix}/database-url"
  description             = "Application PostgreSQL connection URL; not the RDS administrator credentials"
  recovery_window_in_days = 7
  tags                    = { Name = "${var.name_prefix}-database-url" }
}

# 起動時にFargateが取得する。Webとmigrateは同じアプリDBユーザーを使う初期設計。
resource "aws_iam_role_policy" "database_secret" {
  for_each = local.application_workloads

  name = "read-application-database-secret"
  role = aws_iam_role.execution[each.key].name
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["secretsmanager:GetSecretValue"]
      Resource = aws_secretsmanager_secret.database_url.arn
    }]
  })
}
