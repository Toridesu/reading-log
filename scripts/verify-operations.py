#!/usr/bin/env python3
"""DB停止・復旧、バックアップ、別DBでの再ログインを検証する。DBは一時停止するが元データは上書きしない。"""
import hashlib
import json
import os
from pathlib import Path
import runpy
import shutil
import signal
import socket
import subprocess
import sys
import time
import uuid
from http.cookiejar import CookieJar
from urllib.error import HTTPError, URLError
from urllib.parse import parse_qs, urlencode, urlsplit, urlunsplit
from urllib.request import HTTPCookieProcessor, HTTPRedirectHandler, Request, build_opener

ROOT = Path(__file__).resolve().parent.parent
AUTH = runpy.run_path(str(ROOT / 'scripts/verify-login.py'))
DB = runpy.run_path(str(ROOT / 'scripts/local-db.py'))
BASE = 'http://localhost:8080'


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def request(browser, path, form=None):
    req = Request(BASE + path, data=None if form is None else urlencode(form).encode(), headers={'Origin': BASE})
    try:
        response = browser.open(req, timeout=20)
    except HTTPError as error:
        response = error
    with response:
        return response.status, response.headers, response.read().decode()


def login(settings, username):
    jar = CookieJar(policy=AUTH['LocalBrowserCookies']())
    browser = build_opener(HTTPCookieProcessor(jar))
    with browser.open(BASE + '/auth/login', timeout=20) as response:
        page = AUTH['FormParser']()
        page.feed(response.read().decode())
    target = urlsplit(page.login_action or '')
    issuer = urlsplit(settings['OIDC_ISSUER'])
    assert (target.scheme, target.netloc) == (issuer.scheme, issuer.netloc)
    assert target.path.startswith('/realms/reading-log/login-actions/')
    page.fields.update(username=username, password=settings['LOCAL_USER_PASSWORD'])
    with browser.open(Request(page.login_action, urlencode(page.fields).encode()), timeout=20) as response:
        assert response.geturl() == BASE + '/books'
        page = AUTH['FormParser']()
        page.feed(response.read().decode())
    assert page.csrf
    token = next(c.value for c in jar if c.name == 'readinglog_session')
    return {'browser': build_opener(HTTPCookieProcessor(jar), NoRedirect()), 'csrf': page.csrf, 'token': token}


def main():
    os.umask(0o077)
    settings = dict(line.split('=', 1) for line in (ROOT / '.env').read_text().splitlines()
                    if line and not line.startswith('#') and '=' in line)
    assert settings['APP_BASE_URL'] == BASE and settings.get('APP_ADDR', '127.0.0.1:8080') == '127.0.0.1:8080'
    assert urlsplit(settings['OIDC_ISSUER']).hostname == 'localhost'
    # この検証が所有するプロセスだけを終了する。既存のアプリが使うポートなら開始しない。
    with socket.socket() as probe:
        probe.bind(('127.0.0.1', 8080))
    assert (ROOT / 'bin/readinglog').is_file(), 'build bin/readinglog first'
    DB['sql']('readinglog', 'SELECT 1;')
    suffix = uuid.uuid4().hex[:12]
    target = 'readinglog_restore_' + suffix
    corrupt_target = target + '_bad'
    output = ROOT / '.cache/verification' / ('05-operations-' + suffix)
    output.mkdir(parents=True, mode=0o700)
    logfile = output / 'app.jsonl'
    log = logfile.open('x')
    process = None
    db_stopped = False
    restored = False
    created_ids, source_users, restore_users = [], {}, {}
    pending_hash = None
    evidence = {'checks': [], 'http': []}
    started = time.monotonic()

    def start_app(database):
        nonlocal process
        env = dict(os.environ, **settings)
        connection = urlsplit(settings['DATABASE_URL'])
        assert connection.hostname in ('localhost', '127.0.0.1') and connection.path == '/readinglog'
        env['DATABASE_URL'] = urlunsplit(connection._replace(path='/' + database))
        process = subprocess.Popen([str(ROOT / 'bin/readinglog')], env=env, cwd=ROOT, stdout=log, stderr=log)
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if process.poll() is not None:
                raise RuntimeError('app startup failed')
            try:
                if request(build_opener(NoRedirect()), '/healthz')[0] == 200:
                    return
            except (URLError, OSError):
                pass
            time.sleep(0.2)
        raise RuntimeError('app startup timed out')

    def stop_app():
        nonlocal process
        if process is not None:
            if process.poll() is None:
                process.send_signal(signal.SIGINT)
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
            process = None

    def cli(*args, ok=True):
        result = subprocess.run([sys.executable, str(ROOT / 'scripts/local-db.py'), *args],
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=120)
        assert (result.returncode == 0) == ok, 'unexpected DB-tool result'
        return result.stdout.decode().strip()

    def probe_http(label, path, want, form=None):
        status, headers, body = request(source_users['alice']['browser'], path, form)
        assert status == want, label
        evidence['http'].append({'phase': label, 'path': path, 'status': status, 'request_id': headers['X-Request-ID']})
        return body

    try:
        start_app('readinglog')
        for username in ('alice', 'bob'):
            user = source_users[username] = login(settings, username)
            for i in range(2):
                status, headers, _ = request(user['browser'], '/books', {'csrf_token': user['csrf'],
                    'title': f'復元検証 {suffix} {username} {i}', 'status': ('reading', 'finished')[i],
                    'notes': '日本語のメモ 📖\n所有者・本文・改行も照合する。'})
                assert status == 303
                book_id = int(headers['Location'].rsplit('/', 1)[1])
                created_ids.append(book_id)
                user.setdefault('ids', []).append(book_id)
        # 進行中のログインも用意し、復元先ではその行が空になることを確認する。
        status, headers, _ = request(build_opener(NoRedirect()), '/auth/login')
        assert status == 303 or status == 302
        state = parse_qs(urlsplit(headers['Location']).query)['state'][0]
        pending_hash = hashlib.sha256(state.encode()).hexdigest()
        counts = json.loads(DB['sql']('readinglog', 'SELECT json_build_array((SELECT count(*) FROM sessions), (SELECT count(*) FROM login_requests));'))
        assert counts[0] >= 2 and counts[1] >= 1
        evidence['source_auth_rows_before_backup'] = counts
        original = DB['snapshot']('readinglog')
        probe_http('before', '/healthz', 200)
        probe_http('before', '/readyz', 200)
        probe_http('before', '/books', 200)
        app_pid = process.pid
        db_stopped = True
        DB['run'](DB['COMPOSE'] + ['stop', '--timeout', '5', 'db'], stdout=subprocess.DEVNULL)
        probe_http('db_stopped', '/healthz', 200)
        probe_http('db_stopped', '/readyz', 503)
        probe_http('db_stopped', '/books', 503)
        probe_http('db_stopped', '/books', 503, {'csrf_token': source_users['alice']['csrf'],
            'title': '停止中には保存しない', 'status': 'unread', 'notes': ''})
        DB['run'](DB['COMPOSE'] + ['up', '-d', '--wait', 'db'], stdout=subprocess.DEVNULL)
        db_stopped = False
        probe_http('recovered', '/readyz', 200)
        probe_http('recovered', '/books', 200)
        assert process.pid == app_pid and process.poll() is None
        assert DB['snapshot']('readinglog') == original
        evidence['checks'].append('DB停止時200/503、書き込み拒否、同じアプリPID・セッションで回復、元データ不変')
        print('PASS: DB outage, failure logs, recovery without app restart', flush=True)
        stop_app()

        backup_started = time.monotonic()
        backup = Path(cli('backup'))
        evidence['backup_seconds'] = round(time.monotonic() - backup_started, 2)
        evidence['backup_bytes'] = backup.stat().st_size
        assert backup.stat().st_mode & 0o077 == 0
        restore_started = time.monotonic()
        cli('restore', str(backup), target)
        restored = True
        evidence['restore_seconds'] = round(time.monotonic() - restore_started, 2)
        evidence['comparison'] = cli('compare', target).splitlines()
        evidence['checks'].append('users・reading_tasks・goose履歴の全行と採番位置一致、復元先の認証情報は0件')
        # 既存DB・元DBへの復元と、壊れたバックアップを拒否する。
        cli('restore', str(backup), target, ok=False)
        cli('restore', str(backup), 'readinglog', ok=False)
        cli('drop', 'readinglog', ok=False)
        damaged = output / 'damaged.dump'
        shutil.copyfile(backup, damaged)
        shutil.copyfile(backup.with_suffix('.dump.sha256'), damaged.with_suffix('.dump.sha256'))
        with damaged.open('ab') as stream:
            stream.write(b'corruption-test')
        cli('restore', str(damaged), corrupt_target, ok=False)
        assert DB['sql']('postgres', f"SELECT count(*) FROM pg_database WHERE datname='{corrupt_target}';", admin=True).strip() == b'0'
        damaged.unlink()
        damaged.with_suffix('.dump.sha256').unlink()
        assert DB['snapshot']('readinglog') == original
        evidence['checks'].append('既存DB・元DB・破損バックアップの拒否、元DB不変')
        print('PASS: backup, restore, row/owner/content/sequence comparison and unsafe-target rejection', flush=True)

        # .envを書き換えず、子プロセスの接続先だけを復元DBへ切り替える。
        start_app(target)
        for user in source_users.values():
            assert request(user['browser'], '/books')[0] == 303  # 元DBのCookieは復元先では使えない
        for username in ('alice', 'bob'):
            user = restore_users[username] = login(settings, username)
            other = 'bob' if username == 'alice' else 'alice'
            own_id = source_users[username]['ids'][0]
            other_id = source_users[other]['ids'][0]
            status, _, body = request(user['browser'], f'/books/{own_id}')
            assert status == 200 and f'復元検証 {suffix} {username}' in body
            assert '日本語のメモ 📖' in body
            assert request(user['browser'], f'/books/{other_id}')[0] == 404
            form = {'csrf_token': user['csrf'], 'title': '復元後に編集', 'status': 'finished', 'notes': '復元後も保存できる'}
            assert request(user['browser'], f'/books/{other_id}', form)[0] == 404
            assert request(user['browser'], f'/books/{own_id}', form)[0] == 303
            assert request(user['browser'], '/books', form)[0] == 303  # identity sequenceも使用
            assert request(user['browser'], f'/books/{own_id}/delete', {'csrf_token': user['csrf']})[0] == 303
        assert DB['snapshot']('readinglog') == original
        evidence['checks'].append('古いCookie拒否、復元DBへ2ユーザーで再ログイン、所有者制限・追加・更新・削除、元DB不変')
        print('PASS: restored app login, authorization and CRUD; source DB unchanged', flush=True)
        stop_app()
        log.flush()
        text = logfile.read_text()
        rows = [json.loads(line) for line in text.splitlines()]
        by_request = {}
        for row in rows:
            by_request.setdefault(row.get('request_id'), []).append(row)
        for observation in evidence['http']:
            events = by_request[observation['request_id']]
            assert any(r.get('msg') == 'http_request' and r.get('status') == observation['status'] for r in events)
            if observation['phase'] == 'db_stopped' and observation['status'] == 503:
                expected = 'database_unavailable' if observation['path'] == '/readyz' else 'session_unavailable'
                assert any(r.get('msg') == expected for r in events)
        forbidden = [v for k, v in settings.items() if ('PASSWORD' in k or 'SECRET' in k) and v]
        forbidden += [u[key] for u in list(source_users.values()) + list(restore_users.values()) for key in ('csrf', 'token')]
        forbidden += ['日本語のメモ', '復元後も保存できる']
        assert all(value not in text for value in forbidden)
        evidence['checks'].append('request_idで503の原因イベントを追跡、パスワード・セッション・CSRF・メモがログに出ない')
        evidence['elapsed_seconds'] = round(time.monotonic() - started, 2)
        evidence['backup_file'] = str(backup.relative_to(ROOT))
        (output / 'result.json').write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + '\n')
        print(f'Results: {output.relative_to(ROOT)}/result.json', flush=True)
    finally:
        stop_app()
        log.close()
        if db_stopped:
            DB['run'](DB['COMPOSE'] + ['up', '-d', '--wait', 'db'], stdout=subprocess.DEVNULL)
        # この実行で作った本と認証情報だけを消す。既存のユーザーや本は残す。
        queries = []
        if created_ids:
            queries.append('DELETE FROM reading_tasks WHERE id IN (' + ','.join(map(str, created_ids)) + ');')
        for user in source_users.values():
            token_hash = hashlib.sha256(user['token'].encode()).hexdigest()
            queries.append(f"DELETE FROM sessions WHERE token_hash=decode('{token_hash}','hex');")
        if pending_hash:
            queries.append(f"DELETE FROM login_requests WHERE state_hash=decode('{pending_hash}','hex');")
        if queries:
            DB['sql']('readinglog', 'BEGIN;' + ''.join(queries) + 'COMMIT;')
        if restored:
            cli('drop', target)
    print('PASS: test books/sessions removed, restore DB dropped, app stopped, original DB running')


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        import traceback
        line = traceback.extract_tb(error.__traceback__)[-1].lineno
        print(f'Operations verification failed ({type(error).__name__}, line {line}); inspect local logs.', file=sys.stderr)
        sys.exit(1)
