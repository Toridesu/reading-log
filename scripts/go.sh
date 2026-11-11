#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_dir"

export GOCACHE="$project_dir/.cache/go-build"
export GOPATH="$project_dir/.cache/go"
export GOTOOLCHAIN=local

if [[ -x "$project_dir/.tools/go/bin/go" ]]; then
  exec "$project_dir/.tools/go/bin/go" "$@"
fi

if command -v go >/dev/null 2>&1; then
  exec go "$@"
fi

printf '%s\n' 'Goが見つかりません。READMEの開発環境の手順を確認してください。' >&2
exit 1
