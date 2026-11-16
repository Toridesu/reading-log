#!/usr/bin/env bash
# 公式の固定バージョンを検証し、プロジェクト内だけにTerraformを配置する。
set -euo pipefail
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_dir"
python3 - <<'PY'
from pathlib import Path
import hashlib
import io
import platform
import time
import urllib.request
import zipfile

if platform.system() != 'Linux' or platform.machine() != 'x86_64':
    raise SystemExit('このスクリプトはLinux x86_64用です。別環境では公式配布を確認してください。')
version = Path('.terraform-version').read_text().strip()
filename = f'terraform_{version}_linux_amd64.zip'
pins = dict((name, digest) for digest, name in
            (line.split() for line in Path('scripts/terraform-checksums.txt').read_text().splitlines() if line))
expected = pins[filename]
base = f'https://releases.hashicorp.com/terraform/{version}/'
def fetch(url):
    for attempt in range(3):
        try:
            with urllib.request.urlopen(url, timeout=60) as response:
                return response.read()
        except OSError:
            if attempt == 2:
                raise SystemExit('公式配布の取得に失敗しました。接続を確認して再実行してください。')
            time.sleep(1)

published = fetch(base + f'terraform_{version}_SHA256SUMS').decode().splitlines()
if not any(line.split() == [expected, filename] for line in published):
    raise SystemExit('公式チェックサムと固定値が異なります。配置を中止しました。')
archive = fetch(base + filename)
if hashlib.sha256(archive).hexdigest() != expected:
    raise SystemExit('ダウンロードしたファイルのSHA-256が一致しません。配置を中止しました。')
with zipfile.ZipFile(io.BytesIO(archive)) as zipped:
    binary = zipped.read('terraform')
folder = Path('.tools/terraform')
folder.mkdir(parents=True, exist_ok=True)
staged = folder / 'terraform.download'
staged.write_bytes(binary)
staged.chmod(0o755)
staged.replace(folder / 'terraform')
print(f'Terraform {version}: 公式SHA-256と一致。.tools/terraformへ配置しました。')
PY
