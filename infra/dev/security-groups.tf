# 独立したSGルールで通信元・通信先を明示する。戻り通信はSGのステートフルな仕組みで許可される。
resource "aws_security_group" "alb" {
  name        = "${var.name_prefix}-alb"
  description = "HTTPS clients to ALB; ALB to application only"
  vpc_id      = aws_vpc.main.id
  tags        = { Name = "${var.name_prefix}-alb" }
}

resource "aws_security_group" "app" {
  name        = "${var.name_prefix}-app"
  description = "Application receives traffic only from ALB"
  vpc_id      = aws_vpc.main.id
  tags        = { Name = "${var.name_prefix}-app" }
}

resource "aws_security_group" "db" {
  name        = "${var.name_prefix}-db"
  description = "PostgreSQL receives traffic only from application"
  vpc_id      = aws_vpc.main.id
  tags        = { Name = "${var.name_prefix}-db" }
}

# 作成時にできるデフォルトSGも閉じ、誤って使った場合に通信を許可しない。
resource "aws_default_security_group" "main" {
  vpc_id  = aws_vpc.main.id
  ingress = []
  egress  = []
  tags    = { Name = "${var.name_prefix}-default-unused" }
}

resource "aws_vpc_security_group_ingress_rule" "alb_https" {
  security_group_id = aws_security_group.alb.id
  description       = "Public HTTPS entry point"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
}

resource "aws_vpc_security_group_egress_rule" "alb_app" {
  security_group_id            = aws_security_group.alb.id
  description                  = "ALB requests and health checks to application"
  referenced_security_group_id = aws_security_group.app.id
  ip_protocol                  = "tcp"
  from_port                    = 8000
  to_port                      = 8000
}

resource "aws_vpc_security_group_ingress_rule" "app_alb" {
  security_group_id            = aws_security_group.app.id
  description                  = "Application traffic from ALB only"
  referenced_security_group_id = aws_security_group.alb.id
  ip_protocol                  = "tcp"
  from_port                    = 8000
  to_port                      = 8000
}

resource "aws_vpc_security_group_egress_rule" "app_db" {
  security_group_id            = aws_security_group.app.id
  description                  = "PostgreSQL from application"
  referenced_security_group_id = aws_security_group.db.id
  ip_protocol                  = "tcp"
  from_port                    = 5432
  to_port                      = 5432
}

# OIDC・ECR・Logs・Secrets用。SGではドメイン名を絞れないためHTTPSの宛先IPは全体を許可する。
resource "aws_vpc_security_group_egress_rule" "app_https" {
  security_group_id = aws_security_group.app.id
  description       = "HTTPS for OIDC and AWS services including S3"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
}

resource "aws_vpc_security_group_ingress_rule" "db_app" {
  security_group_id            = aws_security_group.db.id
  description                  = "PostgreSQL from application only"
  referenced_security_group_id = aws_security_group.app.id
  ip_protocol                  = "tcp"
  from_port                    = 5432
  to_port                      = 5432
}
