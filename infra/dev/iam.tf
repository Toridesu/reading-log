# 実展開時は接続先アカウントを取得する。ローカルテストではこの取得もmockに置き換える。
data "aws_caller_identity" "current" {}

locals {
  ecs_task_trust = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Action    = "sts:AssumeRole"
      Condition = {
        StringEquals = { "aws:SourceAccount" = data.aws_caller_identity.current.account_id }
        # ECSはこの条件でクラスター名までは絞れない。東京と自分のアカウントを指定する。
        ArnLike = { "aws:SourceArn" = "arn:aws:ecs:ap-northeast-1:${data.aws_caller_identity.current.account_id}:*" }
      }
    }]
  })
}

# Fargateがイメージ取得・ログ送信に使う権限。アプリのコードへは渡さない。
resource "aws_iam_role" "execution" {
  for_each = local.workloads

  name               = "${var.name_prefix}-${each.key}-execution"
  assume_role_policy = local.ecs_task_trust
  tags               = { Workload = each.key }
}

resource "aws_iam_role_policy" "execution" {
  for_each = local.workloads

  name = "image-and-logs"
  role = aws_iam_role.execution[each.key].name
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "ECRAuthentication"
        Effect = "Allow"
        Action = ["ecr:GetAuthorizationToken"]
        # このAPIはリポジトリARNを指定できない。pull本体は次のStatementで絞る。
        Resource = "*"
      },
      {
        Sid    = "PullOwnImage"
        Effect = "Allow"
        Action = [
          "ecr:BatchCheckLayerAvailability",
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchGetImage",
        ]
        Resource = aws_ecr_repository.app[each.key].arn
      },
      {
        Sid      = "WriteOwnLogs"
        Effect   = "Allow"
        Action   = ["logs:CreateLogStream", "logs:PutLogEvents"]
        Resource = "${aws_cloudwatch_log_group.app[each.key].arn}:log-stream:*"
      },
    ]
  })
}

# GoからAWS APIを呼ぶときのロール。Web/migrateは許可なし。初期化だけにSecret操作を別途付ける。
resource "aws_iam_role" "task" {
  for_each = local.workloads

  name               = "${var.name_prefix}-${each.key}-task"
  assume_role_policy = local.ecs_task_trust
  tags               = { Workload = each.key }
}
