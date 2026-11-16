# WebとDB変更用のイメージを別々に保管する。同じタグの上書きと、中身を含めた強制削除を防ぐ。
locals {
  workloads = toset(["web", "migrate"])
}

resource "aws_ecr_repository" "app" {
  for_each = local.workloads

  name                 = "${var.name_prefix}/${each.key}"
  image_tag_mutability = "IMMUTABLE"
  force_delete         = false

  encryption_configuration {
    encryption_type = "AES256"
  }

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = { Name = "${var.name_prefix}-${each.key}", Workload = each.key }
}
