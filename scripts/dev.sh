#!/usr/bin/env bash
# .envのローカル設定を読み込み、go.shへ渡す。アプリ起動・マイグレーション・DBテストに使う。
set -euo pipefail
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_dir"
if [[ ! -f .env ]]; then
  printf '%s\n' '先に bash scripts/setup-local.sh を実行してください。' >&2
  exit 1
fi

# 自分で生成・編集したローカル設定を読み込んでGoへ渡す。
set -a
source .env
set +a
exec bash scripts/go.sh "$@"
