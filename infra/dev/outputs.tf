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
