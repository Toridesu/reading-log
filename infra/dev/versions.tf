# CLIとAWSプロバイダーを固定し、更新は検査後に取り込む。
terraform {
  required_version = "= 1.17.2"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "= 6.73.0"
    }
  }
}
