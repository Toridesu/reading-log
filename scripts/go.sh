#!/usr/bin/env bash
# コマンドの失敗・未定義変数・パイプ途中の失敗を見逃さず終了する。
set -euo pipefail

# 実行した場所にかかわらず、このスクリプトのあるプロジェクトへ移動する。
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_dir"

# ビルドキャッシュとGoの作業領域をプロジェクト内へ置く（Git管理対象外）。
export GOCACHE="$project_dir/.cache/go-build"
export GOPATH="$project_dir/.cache/go"
# 不足するGoのバージョンを自動ダウンロードせず、用意したSDKを使う。
export GOTOOLCHAIN=local

# プロジェクト内のSDKを優先。"$@"は渡された引数（test ./...等）をそのまま渡す。
# execは、このシェルをGoコマンドに置き換える。
if [[ -x "$project_dir/.tools/go/bin/go" ]]; then
  exec "$project_dir/.tools/go/bin/go" "$@"
fi

# プロジェクト内にSDKがなければ、通常のPATHからGoを探す。
if command -v go >/dev/null 2>&1; then
  exec go "$@"
fi

printf '%s\n' 'Goが見つかりません。READMEの開発環境の手順を確認してください。' >&2
exit 1
