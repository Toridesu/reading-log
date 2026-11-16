variable "name_prefix" {
  description = "リソース名の先頭。個人開発のdev環境を識別する。"
  type        = string
  default     = "reading-log-dev"
  nullable    = false

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{2,23}$", var.name_prefix))
    error_message = "name_prefixは英小文字で始まる3〜24文字の英小文字・数字・ハイフンにしてください。"
  }
}

variable "vpc_cidr" {
  description = "初期版は10.0.0.0/8内の/16を使い、重ならない6つの/24へ分割する。"
  type        = string
  default     = "10.20.0.0/16"
  nullable    = false

  validation {
    condition     = can(cidrnetmask(var.vpc_cidr)) && can(regex("^10\\.[0-9]+\\.0\\.0/16$", var.vpc_cidr))
    error_message = "vpc_cidrは10.x.0.0/16形式の有効なIPv4 CIDRにしてください。"
  }
}

variable "availability_zones" {
  description = "東京の異なる2つのAZ。先頭の公開サブネットへNATを置く。実展開前にアカウントで利用可能か確認する。"
  type        = list(string)
  default     = ["ap-northeast-1a", "ap-northeast-1c"]
  nullable    = false

  validation {
    condition = length(var.availability_zones) == 2 && length(distinct(var.availability_zones)) == 2 && alltrue([
      for az in var.availability_zones : can(regex("^ap-northeast-1[a-z]$", az))
    ])
    error_message = "availability_zonesは東京の異なるAZを2つ指定してください。"
  }
}
