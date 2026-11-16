# 実AWSの代わりに架空のアカウント・ARNを使い、IAMの許可範囲を確認する。
mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
}

# 同じARNが入ると取り違えを見逃すため、Webとmigrateで別の値にする。
override_resource {
  target = aws_ecr_repository.app["web"]
  values = { arn = "arn:aws:ecr:ap-northeast-1:123456789012:repository/reading-log-dev/web" }
}
override_resource {
  target = aws_ecr_repository.app["migrate"]
  values = { arn = "arn:aws:ecr:ap-northeast-1:123456789012:repository/reading-log-dev/migrate" }
}
override_resource {
  target = aws_cloudwatch_log_group.app["web"]
  values = { arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:/ecs/reading-log-dev/web" }
}
override_resource {
  target = aws_cloudwatch_log_group.app["migrate"]
  values = { arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:/ecs/reading-log-dev/migrate" }
}

run "artifact_and_log_storage" {
  command = apply

  assert {
    condition = toset(keys(aws_ecr_repository.app)) == toset(["web", "migrate"]) && alltrue([
      for workload, repository in aws_ecr_repository.app :
      repository.name == "${var.name_prefix}/${workload}" &&
      repository.image_tag_mutability == "IMMUTABLE" && !repository.force_delete &&
      repository.encryption_configuration[0].encryption_type == "AES256" &&
      repository.image_scanning_configuration[0].scan_on_push
    ])
    error_message = "Webとmigrateのイメージを分け、タグ上書き・強制削除を無効にし、暗号化・push時検査を設定してください。"
  }
  assert {
    condition = toset(keys(aws_cloudwatch_log_group.app)) == toset(["web", "migrate"]) && alltrue([
      for workload, group in aws_cloudwatch_log_group.app :
      group.name == "/ecs/${var.name_prefix}/${workload}" && group.retention_in_days == 14 &&
      group.log_group_class == "STANDARD" && !group.skip_destroy && !group.deletion_protection_enabled
    ])
    error_message = "Webとmigrateのログを分け、14日保持・環境削除時の削除を設定してください。"
  }
  assert {
    condition = (aws_ecs_cluster_capacity_providers.app.cluster_name == aws_ecs_cluster.app.name &&
      aws_ecs_cluster_capacity_providers.app.capacity_providers == toset(["FARGATE"]) &&
      one(aws_ecs_cluster_capacity_providers.app.default_capacity_provider_strategy).capacity_provider == "FARGATE" &&
    one(aws_ecs_cluster_capacity_providers.app.default_capacity_provider_strategy).weight == 1)
    error_message = "クラスターの初期実行先は通常のFargateだけにしてください。"
  }
}

run "execution_permissions" {
  command = apply

  assert {
    condition = toset(keys(aws_iam_role_policy.execution)) == toset(["web", "migrate"]) && alltrue([
      for workload, policy in aws_iam_role_policy.execution :
      policy.role == aws_iam_role.execution[workload].name &&
      policy.role != aws_iam_role.task[workload].name &&
      length(jsondecode(policy.policy).Statement) == 3 &&
      alltrue([for statement in jsondecode(policy.policy).Statement : statement.Effect == "Allow"])
    ])
    error_message = "イメージ・ログの権限は各実行ロールへ付与し、アプリのタスクロールへ付与しないでください。"
  }
  assert {
    condition = alltrue([
      for policy in aws_iam_role_policy.execution :
      toset(jsondecode(policy.policy).Statement[0].Action) == toset(["ecr:GetAuthorizationToken"]) &&
      jsondecode(policy.policy).Statement[0].Resource == "*"
    ])
    error_message = "Resource全体の許可はECRの認証トークン取得だけにしてください。"
  }
  assert {
    condition = alltrue([
      for workload, policy in aws_iam_role_policy.execution :
      toset(jsondecode(policy.policy).Statement[1].Action) == toset(["ecr:BatchCheckLayerAvailability", "ecr:GetDownloadUrlForLayer", "ecr:BatchGetImage"]) &&
      jsondecode(policy.policy).Statement[1].Resource == aws_ecr_repository.app[workload].arn &&
      jsondecode(policy.policy).Statement[1].Resource != aws_ecr_repository.app[workload == "web" ? "migrate" : "web"].arn
    ])
    error_message = "pullは自分のECRだけに限定し、push・削除や他方のイメージ取得を許可しないでください。"
  }
  assert {
    condition = alltrue([
      for workload, policy in aws_iam_role_policy.execution :
      toset(jsondecode(policy.policy).Statement[2].Action) == toset(["logs:CreateLogStream", "logs:PutLogEvents"]) &&
      jsondecode(policy.policy).Statement[2].Resource == "arn:aws:logs:ap-northeast-1:123456789012:log-group:/ecs/reading-log-dev/${workload}:log-stream:*"
    ])
    error_message = "ログ送信は自分のログストリームだけに限定し、グループ作成や他方への送信を許可しないでください。"
  }
}

run "ecs_role_trust" {
  command = apply

  assert {
    condition = (length(aws_iam_role.execution) == 2 && length(aws_iam_role.task) == 2 &&
    length(toset(concat([for role in aws_iam_role.execution : role.name], [for role in aws_iam_role.task : role.name]))) == 4)
    error_message = "Web/migrateの実行ロール・タスクロールを4つに分離してください。"
  }
  assert {
    condition = alltrue([
      for role in concat(values(aws_iam_role.execution), values(aws_iam_role.task)) :
      length(jsondecode(role.assume_role_policy).Statement) == 1 &&
      jsondecode(role.assume_role_policy).Statement[0].Effect == "Allow" &&
      jsondecode(role.assume_role_policy).Statement[0].Principal == { Service = "ecs-tasks.amazonaws.com" } &&
      jsondecode(role.assume_role_policy).Statement[0].Action == "sts:AssumeRole" &&
      jsondecode(role.assume_role_policy).Statement[0].Condition.StringEquals["aws:SourceAccount"] == "123456789012" &&
      jsondecode(role.assume_role_policy).Statement[0].Condition.ArnLike["aws:SourceArn"] == "arn:aws:ecs:ap-northeast-1:123456789012:*"
    ])
    error_message = "信頼先をECSタスクにし、東京・自分のアカウントを条件に指定してください。"
  }
}

run "trust_follows_deployment_account" {
  command = plan

  override_data {
    target = data.aws_caller_identity.current
    values = { account_id = "987654321098" }
  }

  assert {
    condition = alltrue([
      for role in concat(values(aws_iam_role.execution), values(aws_iam_role.task)) :
      jsondecode(role.assume_role_policy).Statement[0].Condition.StringEquals["aws:SourceAccount"] == "987654321098" &&
      jsondecode(role.assume_role_policy).Statement[0].Condition.ArnLike["aws:SourceArn"] == "arn:aws:ecs:ap-northeast-1:987654321098:*"
    ])
    error_message = "アカウントIDを固定値にせず、展開先のIDを信頼条件へ反映してください。"
  }
}
