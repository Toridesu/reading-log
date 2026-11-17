# 初期化用GoコードだけがSecretを取得・保存する。実行ロールやWebへは付与しない。
resource "aws_iam_role_policy" "bootstrap_secrets" {
  name = "initialize-application-database"
  role = aws_iam_role.task["bootstrap"].name
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["secretsmanager:GetSecretValue"]
        Resource = aws_db_instance.app.master_user_secret[0].secret_arn
      },
      {
        Effect   = "Allow"
        Action   = ["secretsmanager:GetSecretValue", "secretsmanager:PutSecretValue"]
        Resource = aws_secretsmanager_secret.database_url.arn
      },
    ]
  })
}
