#!/usr/bin/env bash
# AWS認証を使わず、整形・構文・mockテストを確認する。initの配布物取得にはインターネットを使う。
set -euo pipefail
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_dir"
python3 - <<'PY'
import os
import subprocess

# 手元にAWS設定があっても検査へ渡さない。Terraformへ追加引数や変数を注入する環境変数も外す。
env = {key: value for key, value in os.environ.items()
       if not key.startswith(('AWS_', 'TF_VAR_', 'TF_CLI_ARGS'))}
env.update(AWS_CONFIG_FILE='/dev/null', AWS_SHARED_CREDENTIALS_FILE='/dev/null', AWS_EC2_METADATA_DISABLED='true', TF_IN_AUTOMATION='1')
wrapper = ['bash', 'scripts/terraform.sh']
for arguments in [
    ['fmt', '-check', '-recursive', 'infra'],
    ['-chdir=infra/dev', 'init', '-backend=false', '-input=false', '-lockfile=readonly', '-no-color'],
    ['-chdir=infra/dev', 'validate', '-no-color'],
    ['-chdir=infra/dev', 'test', '-no-color'],
]:
    subprocess.run(wrapper + arguments, env=env, check=True)
PY
