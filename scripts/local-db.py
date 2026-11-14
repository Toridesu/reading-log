#!/usr/bin/env python3
"""ローカルComposeのDBをバックアップ・別DBへ復元・照合する。Python標準ライブラリだけで動く。"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from datetime import datetime, timezone

ROOT = Path(__file__).resolve().parent.parent
COMPOSE = ['docker', 'compose', '--project-directory', str(ROOT), '-f', str(ROOT / 'compose.yaml')]
SOURCE = 'readinglog'


def run(args, **kwargs):
    result = subprocess.run(args, stderr=subprocess.PIPE, timeout=120, **kwargs)
    if result.returncode:
        # psqlのエラーにはデータが含まれ得るため、そのまま端末へ出さない。
        raise RuntimeError('DB操作に失敗しました。DBの稼働・接続設定・対象DBを確認してください。')
    return result


def db_command(program, database, *options, admin=False):
    # パスワードはコンテナ内の環境変数から読み、コマンド引数へ展開しない。
    role, password = ('postgres', 'POSTGRES_PASSWORD') if admin else ('readinglog', 'APP_DB_PASSWORD')
    return COMPOSE + ['exec', '-T', 'db', 'sh', '-c',
        f'export PGPASSWORD="${password}" PGCONNECT_TIMEOUT=5 PGTZ=UTC; exec "$@"',
        'db-tool', program, '-h', '127.0.0.1', '-U', role, '-w', *options, '-d', database]


def sql(database, query, admin=False):
    return run(db_command('psql', database, '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1', admin=admin),
               input=query.encode(), stdout=subprocess.PIPE).stdout


def restore_name(value):
    # オプション・接続URL・元DBの名前を復元先として受け付けない。
    if not re.fullmatch(r'readinglog_restore_[a-z0-9_]{1,40}', value):
        raise ValueError('DB名は readinglog_restore_ に英小文字・数字・_ を1〜40文字続けてください。')
    return value


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def backup():
    parent = ROOT / '.cache' / 'backups'
    parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ_')
    directory = Path(tempfile.mkdtemp(prefix=stamp, dir=parent))
    path = directory / 'readinglog.dump'
    try:
        with path.open('xb') as stream:
            run(db_command('pg_dump', SOURCE, '--format=custom', '--no-owner', '--no-acl',
                '--exclude-table-data=public.sessions', '--exclude-table-data=public.login_requests'), stdout=stream)
        path.with_suffix('.dump.sha256').write_text(digest(path) + '\n')
    except Exception:
        path.unlink(missing_ok=True)
        raise
    print(path)


def restore(path, target):
    target = restore_name(target)
    path = Path(path).resolve()
    expected = path.with_suffix('.dump.sha256').read_text().strip()
    if not re.fullmatch(r'[0-9a-f]{64}', expected) or digest(path) != expected:
        raise ValueError('バックアップのSHA-256が一致しません。復元先は作成していません。')
    with path.open('rb') as stream:
        run(COMPOSE + ['exec', '-T', 'db', 'pg_restore', '--list'], stdin=stream, stdout=subprocess.DEVNULL)
    # CREATE DATABASEは既存DBなら失敗する。--cleanや--createで元DBへ戻す操作は使わない。
    sql('postgres', f'CREATE DATABASE "{target}" OWNER readinglog TEMPLATE template0;', admin=True)
    try:
        with path.open('rb') as stream:
            run(db_command('pg_restore', target, '--single-transaction', '--exit-on-error', '--no-owner', '--no-acl'),
                stdin=stream, stdout=subprocess.DEVNULL)
        counts = json.loads(sql(target, "SELECT json_build_array((SELECT count(*) FROM sessions), (SELECT count(*) FROM login_requests));"))
        if counts != [0, 0]:
            raise RuntimeError('認証情報の行が含まれるため、このバックアップは使えません。')
        sql(target, 'ANALYZE;')
    except Exception:
        # この呼び出しで作ったDBだけを片付ける。他のDBや接続中のDBを強制削除しない。
        try:
            drop(target)
        except Exception:
            print(f'復元に失敗しました。残ったDBを確認してください: {target}', file=sys.stderr)
        raise
    print(f'Restored: {target} (sessions=0, login_requests=0)')


def snapshot(database):
    # 件数だけでなく、所有者・本文・時刻を含む全行と採番位置を照合する。内容は端末へ出さない。
    tables = ['users', 'reading_tasks', 'goose_db_version']
    pairs = []
    for table in tables:
        pairs.append(f"'{table}', COALESCE((SELECT json_agg(t ORDER BY id) FROM public.{table} t), '[]'::json)")
    for sequence in ['users_id_seq', 'reading_tasks_id_seq', 'goose_db_version_id_seq']:
        pairs.append(f"'{sequence}', (SELECT json_build_object('last_value', last_value, 'is_called', is_called) FROM public.{sequence})")
    return json.loads(sql(database, 'SELECT json_build_object(' + ','.join(pairs) + ');'))


def compare(target):
    target = restore_name(target)
    before, after = snapshot(SOURCE), snapshot(target)
    if before != after:
        changed = [name for name in before if before[name] != after[name]]
        raise RuntimeError('不一致: ' + ', '.join(changed) + '。バックアップ後に元DBが更新されていないか確認してください。')
    if json.loads(sql(target, 'SELECT json_build_array((SELECT count(*) FROM sessions), (SELECT count(*) FROM login_requests));')) != [0, 0]:
        raise RuntimeError('復元先にログイン情報が残っています。')
    for name in ['users', 'reading_tasks', 'goose_db_version']:
        checksum = hashlib.sha256(json.dumps(after[name], ensure_ascii=False, sort_keys=True).encode()).hexdigest()
        print(f'MATCH {name}: rows={len(after[name])} sha256={checksum}')
    print('MATCH sequences; sessions=0; login_requests=0')


def drop(target):
    target = restore_name(target)
    sql('postgres', f'DROP DATABASE "{target}";', admin=True)
    print(f'Dropped: {target}')


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    commands.add_parser('backup', help='readinglogを.cache/backupsへ保存')
    command = commands.add_parser('restore', help='自分で作ったバックアップを新しいDBへ復元')
    command.add_argument('backup_file')
    command.add_argument('database')
    for name, help_text in [('compare', '元DBと復元先を照合（アプリ等からの書き込みを止めて実行）'),
                            ('drop', '名前を指定した復元用DBを削除')]:
        command = commands.add_parser(name, help=help_text)
        command.add_argument('database')
    args = parser.parse_args()
    if args.command == 'backup':
        backup()
    elif args.command == 'restore':
        restore(args.backup_file, args.database)
    elif args.command == 'compare':
        compare(args.database)
    elif args.command == 'drop':
        drop(args.database)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, RuntimeError) as error:
        print(error, file=sys.stderr)
        sys.exit(1)
    except (OSError, subprocess.SubprocessError):
        print('ローカルDB操作を完了できませんでした。ファイル・Docker・DBの状態を確認してください。', file=sys.stderr)
        sys.exit(1)
