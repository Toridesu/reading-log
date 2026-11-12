#!/usr/bin/env bash
# 初回の設定準備。ランダムなDBパスワードを含む.envを作る。既存ファイルは上書きしない。
set -euo pipefail
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_dir"

python3 - <<'PYTHON'
from pathlib import Path
import os
import secrets

path = Path('.env')
if path.exists():
    print('.env は既にあります。上書きしません。')
    raise SystemExit(0)

admin = secrets.token_hex(24)
app = secrets.token_hex(24)
values = [
    'APP_ADDR=127.0.0.1:8080',
    'DB_PORT=5432',
    f'POSTGRES_PASSWORD={admin}',
    f'APP_DB_PASSWORD={app}',
    f'DATABASE_URL=postgres://readinglog:{app}@127.0.0.1:5432/readinglog?sslmode=disable',
    f'TEST_DATABASE_URL=postgres://readinglog:{app}@127.0.0.1:5432/readinglog_test?sslmode=disable',
]
# 0o600は、このファイルの所有者だけが読み書きできる権限。
fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as f:
    f.write('\n'.join(values) + '\n')
print('.env を作成しました（ローカル開発専用・Git管理対象外）。')
PYTHON
