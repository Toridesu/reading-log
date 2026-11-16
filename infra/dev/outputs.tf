# 次に追加するALB・ECS・RDSから使うIDと、設計確認用のアドレス一覧。
output "vpc_id" {
  value = aws_vpc.main.id
}

output "subnet_ids" {
  value = {
    public = [for subnet in aws_subnet.public : subnet.id]
    app    = [for subnet in aws_subnet.app : subnet.id]
    db     = [for subnet in aws_subnet.db : subnet.id]
  }
}

output "security_group_ids" {
  value = {
    alb = aws_security_group.alb.id
    app = aws_security_group.app.id
    db  = aws_security_group.db.id
  }
}

output "subnet_cidrs" {
  value = {
    public = [for subnet in aws_subnet.public : subnet.cidr_block]
    app    = [for subnet in aws_subnet.app : subnet.cidr_block]
    db     = [for subnet in aws_subnet.db : subnet.cidr_block]
  }
}

# 後続のタスク定義・デプロイ手順で使う。秘密情報は出力しない。
output "ecr_repositories" {
  value = {
    for workload, repository in aws_ecr_repository.app : workload => {
      name = repository.name
      arn  = repository.arn
      url  = repository.repository_url
    }
  }
}

output "ecs_cluster" {
  value = {
    name = aws_ecs_cluster.app.name
    arn  = aws_ecs_cluster.app.arn
  }
}

output "log_group_names" {
  value = { for workload, group in aws_cloudwatch_log_group.app : workload => group.name }
}

output "ecs_role_arns" {
  value = {
    for workload in local.workloads : workload => {
      execution = aws_iam_role.execution[workload].arn
      task      = aws_iam_role.task[workload].arn
    }
  }
}
