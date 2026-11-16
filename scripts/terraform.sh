#!/usr/bin/env bash
# プロジェクト内のTerraformを使う。CLIやプロバイダーのキャッシュはGitへ入れない。
set -euo pipefail
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_dir"
if [[ ! -x .tools/terraform/terraform ]]; then
  printf '%s\n' '先に bash scripts/setup-terraform.sh を実行してください。' >&2
  exit 1
fi
mkdir -p .cache/terraform-plugins
export TF_PLUGIN_CACHE_DIR="$project_dir/.cache/terraform-plugins"
export CHECKPOINT_DISABLE=1
exec .tools/terraform/terraform "$@"
