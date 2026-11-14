#!/usr/bin/env python3
"""ローカルのKeycloakログイン画面を通して、2ユーザーの認証とログアウトを確認する。"""
from html.parser import HTMLParser
from http.cookiejar import CookieJar, DefaultCookiePolicy
from pathlib import Path
from urllib.error import HTTPError
from urllib.parse import urlencode, urlparse
from urllib.request import HTTPCookieProcessor, HTTPRedirectHandler, Request, build_opener
import sys


class FormParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.login_action = None
        self.fields = {}
        self.csrf = None
        self.in_login = False

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == 'form' and attrs.get('id') == 'kc-form-login':
            self.in_login = True
            self.login_action = attrs.get('action')
        if tag == 'input' and attrs.get('name'):
            if self.in_login:
                self.fields[attrs['name']] = attrs.get('value', '')
            if attrs['name'] == 'csrf_token':
                self.csrf = attrs.get('value')

    def handle_endtag(self, tag):
        if tag == 'form':
            self.in_login = False


class LocalBrowserCookies(DefaultCookiePolicy):
    # ブラウザー同様、localhostのHTTPに限ってSecure Cookieを許容する。
    # urllibの標準動作との差を埋め、KeycloakのCookie設定は変更しない。
    def return_ok_secure(self, cookie, request):
        if urlparse(request.full_url).hostname == 'localhost':
            return True
        return super().return_ok_secure(cookie, request)


class Redirects(HTTPRedirectHandler):
    def __init__(self):
        self.callback = None

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        if urlparse(newurl).path == '/auth/callback':
            self.callback = newurl
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def check(condition, message):
    if not condition:
        raise RuntimeError(message)


def main():
    root = Path(__file__).resolve().parent.parent
    settings = dict(line.split('=', 1) for line in (root / '.env').read_text().splitlines()
                    if line and not line.startswith('#') and '=' in line)
    base = settings['APP_BASE_URL']
    issuer = urlparse(settings['OIDC_ISSUER'])
    # 生成した開発用パスワードを外部へ送らないよう、接続先をローカルに限定する。
    check(urlparse(base).hostname == 'localhost' and issuer.hostname == 'localhost', 'local URLs required')
    for username in ('alice', 'bob'):
        jar, redirects = CookieJar(policy=LocalBrowserCookies()), Redirects()
        browser = build_opener(HTTPCookieProcessor(jar), redirects)
        with browser.open(base + '/books', timeout=15) as response:
            check(response.geturl() == base + '/', 'guest accessed protected page')
        with browser.open(base + '/auth/login', timeout=15) as response:
            page = FormParser()
            page.feed(response.read().decode())
        target = urlparse(page.login_action or '')
        check(target.scheme == issuer.scheme and target.netloc == issuer.netloc, 'unexpected login destination')
        check(target.path.startswith('/realms/reading-log/login-actions/'), 'unexpected login form')
        page.fields.update(username=username, password=settings['LOCAL_USER_PASSWORD'])
        with browser.open(Request(page.login_action, urlencode(page.fields).encode()), timeout=15) as response:
            body = response.read().decode()
            check(response.geturl() == base + '/books' and username in body, 'login did not reach books')
        authenticated = FormParser()
        authenticated.feed(body)
        check(bool(authenticated.csrf), 'logout form missing')
        session = next((c for c in jar if c.name == 'readinglog_session'), None)
        check(session is not None, 'session cookie missing')
        saved_cookie = session.name + '=' + session.value
        check(bool(redirects.callback), 'callback was not observed')
        try:
            browser.open(redirects.callback, timeout=15)
            raise RuntimeError('callback replay accepted')
        except HTTPError as error:
            check(error.code == 400, 'unexpected callback rejection')
        try:
            browser.open(Request(base + '/auth/logout', urlencode({'csrf_token': 'wrong'}).encode(),
                                 headers={'Origin': base}), timeout=15)
            raise RuntimeError('invalid CSRF accepted')
        except HTTPError as error:
            check(error.code == 403, 'unexpected CSRF rejection')
        with browser.open(Request(base + '/auth/logout', urlencode({'csrf_token': authenticated.csrf}).encode(),
                                  headers={'Origin': base}), timeout=15) as response:
            check(response.geturl() == base + '/', 'logout did not return home')
        check(not any(c.name == 'readinglog_session' for c in jar), 'session cookie not cleared')
        # 保存しておいた古いCookieを送り直しても、DB側で失効しているのでアクセスできない。
        with build_opener().open(Request(base + '/books', headers={'Cookie': saved_cookie}), timeout=15) as response:
            check(response.geturl() == base + '/', 'revoked session accepted')
        print(f'{username}: login, callback replay rejection, CSRF rejection, logout, revoked-cookie rejection OK')


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # URLに認証コードが含まれるため、例外の詳細をそのまま出さない。
        status = f', status={error.code}' if isinstance(error, HTTPError) else ''
        print(f'Login verification failed ({type(error).__name__}{status}); check application/Keycloak logs.', file=sys.stderr)
        sys.exit(1)
