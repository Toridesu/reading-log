# 初期構成は東京。認証情報はコードに書かない。今回の検査ではmock providerを使う。
provider "aws" {
  region = "ap-northeast-1"

  default_tags {
    tags = {
      Project     = "reading-log"
      Environment = "dev"
      ManagedBy   = "Terraform"
    }
  }
}
