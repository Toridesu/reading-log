#!/usr/bin/env bash
# 初回の設定準備。ランダムなDBパスワードを含む.envを作る。既存の値は保持し、足りない設定だけ追加する。
set -euo pipefail
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_dir"

python3 - <<'PYTHON'
from pathlib import Path
import os
import secrets

path = Path('.env')
existing = path.read_text() if path.exists() else ''
values = dict(line.split('=', 1) for line in existing.splitlines()
              if line and not line.startswith('#') and '=' in line)
app_password = values.get('APP_DB_PASSWORD', secrets.token_hex(24))
defaults = {
    'APP_ADDR': '127.0.0.1:8080',
    'DB_PORT': '5432',
    'POSTGRES_PASSWORD': secrets.token_hex(24),
    'APP_DB_PASSWORD': app_password,
    'DATABASE_URL': f'postgres://readinglog:{app_password}@127.0.0.1:5432/readinglog?sslmode=disable',
    'TEST_DATABASE_URL': f'postgres://readinglog:{app_password}@127.0.0.1:5432/readinglog_test?sslmode=disable',
    'APP_BASE_URL': 'http://localhost:8080',
    'KEYCLOAK_PORT': '8081',
    'OIDC_ISSUER': 'http://localhost:8081/realms/reading-log',
    'OIDC_CLIENT_ID': 'reading-log',
    'OIDC_CLIENT_SECRET': secrets.token_hex(32),
    'KEYCLOAK_ADMIN_PASSWORD': secrets.token_hex(24),
    'LOCAL_USER_PASSWORD': secrets.token_hex(16),
}
missing = [f'{key}={value}' for key, value in defaults.items() if key not in values]
if missing:
    # 0o600は、このファイルの所有者だけが読み書きできる権限。
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    with os.fdopen(fd, 'w') as f:
        if existing and not existing.endswith('\n'):
            f.write('\n')
        f.write('\n'.join(missing) + '\n')
    print('.env に不足していた設定を追加しました（Git管理対象外）。')
else:
    print('.env の設定は揃っています。変更しません。')
PYTHON
