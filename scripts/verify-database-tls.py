#!/usr/bin/env python3
"""一時CAと使い捨てDBでGoのTLSを検証する。既存ComposeのDBや.envは変更しない。"""
from pathlib import Path
import argparse
import os
import secrets
import subprocess
import tempfile
import time
from urllib.parse import urlencode

ROOT = Path(__file__).resolve().parents[1]
os.chdir(ROOT)


def quiet(arguments, **kwargs):
    return subprocess.run(arguments, check=True, stdout=subprocess.DEVNULL,
                          stderr=subprocess.DEVNULL, **kwargs)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--bootstrap', action='store_true', help='DB初期化と再実行も検証する')
    args = parser.parse_args()
    database = 'readinglog' if args.bootstrap else 'readinglog_tls_test'
    cache = ROOT / '.cache'
    cache.mkdir(exist_ok=True)
    name = 'reading-log-tls-' + secrets.token_hex(5)
    with tempfile.TemporaryDirectory(prefix='tls-', dir=cache) as temporary:
        folder = Path(temporary)
        folder.chmod(0o755)
        for root in ['ca', 'untrusted']:
            quiet(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
                   '-subj', '/CN=Reading Log TLS Test ' + root,
                   '-addext', 'basicConstraints=critical,CA:TRUE',
                   '-keyout', str(folder / (root + '.key')), '-out', str(folder / (root + '.pem'))])
        quiet(['openssl', 'req', '-newkey', 'rsa:2048', '-nodes', '-subj', '/CN=localhost',
               '-keyout', str(folder / 'server.key'), '-out', str(folder / 'server.csr')])
        (folder / 'server.ext').write_text('subjectAltName=DNS:localhost\nbasicConstraints=CA:FALSE\nextendedKeyUsage=serverAuth\n')
        quiet(['openssl', 'x509', '-req', '-in', str(folder / 'server.csr'), '-days', '1',
               '-CA', str(folder / 'ca.pem'), '-CAkey', str(folder / 'ca.key'), '-CAcreateserial',
               '-extfile', str(folder / 'server.ext'), '-out', str(folder / 'server.pem')])
        (folder / 'pg_hba.conf').write_text('local all all trust\nhostssl all all 0.0.0.0/0 scram-sha-256\nhostssl all all ::/0 scram-sha-256\n')
        password = secrets.token_hex(24)
        env = {key: value for key, value in os.environ.items() if not key.startswith(('PG', 'AWS_'))}
        env.update(POSTGRES_PASSWORD=password, AWS_CONFIG_FILE='/dev/null',
                   AWS_SHARED_CREDENTIALS_FILE='/dev/null', AWS_EC2_METADATA_DISABLED='true')
        created = False
        try:
            # 秘密鍵はコンテナ内へコピーしてpostgres所有の600にする。
            quiet(['docker', 'run', '-d', '--name', name,
                   '-p', '127.0.0.1::5432', '-e', 'POSTGRES_PASSWORD',
                   '-e', 'POSTGRES_DB=' + database,
                   '--mount', f'type=bind,src={folder},dst=/tls,readonly',
                   'postgres:18.6-bookworm', 'bash', '-c',
                   'install -o postgres -g postgres -m 600 /tls/server.key /tmp/server.key && '
                   'exec docker-entrypoint.sh postgres -c ssl=on -c ssl_cert_file=/tls/server.pem '
                   '-c ssl_key_file=/tmp/server.key -c hba_file=/tls/pg_hba.conf'], env=env)
            created = True
            deadline = time.monotonic() + 90
            while time.monotonic() < deadline:
                ready = subprocess.run(['docker', 'exec', name, 'pg_isready', '-h', '127.0.0.1',
                                        '-U', 'postgres', '-d', database],
                                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                if ready.returncode == 0:
                    break
                time.sleep(1)
            else:
                raise RuntimeError('Disposable PostgreSQL did not become ready')
            port = subprocess.check_output(['docker', 'port', name, '5432/tcp'], text=True).strip().rsplit(':', 1)[1]
            query = urlencode({'sslmode': 'verify-full', 'sslrootcert': str(folder / 'ca.pem')})
            url = f'postgres://postgres:{password}@localhost:{port}/{database}?{query}'
            if args.bootstrap:
                env['BOOTSTRAP_TEST_ADMIN_URL'] = url
                package, test = './internal/bootstrap', '^TestBootstrapIntegration$'
            else:
                env.update(TLS_TEST_DATABASE_URL=url, TLS_TEST_UNTRUSTED_CA=str(folder / 'untrusted.pem'))
                package, test = './internal/database', '^TestTLSIntegration$'
            env['DATABASE_REQUIRE_TLS'] = 'true'
            subprocess.run(['bash', 'scripts/go.sh', 'test', package,
                            '-run', test, '-count=1', '-v'], env=env, check=True)
            print('PASS: disposable PostgreSQL verification')
        finally:
            # この検証で決めたコンテナ名だけを削除し、匿名データボリュームも残さない。
            subprocess.run(['docker', 'rm', '-f', '-v', name], check=created,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    print('PASS: disposable container, data volume and test keys removed')


if __name__ == '__main__':
    main()
