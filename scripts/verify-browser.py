#!/usr/bin/env python3
"""任意のブラウザー検証。Playwright/Chromiumがある環境でログイン画面とCookieの動作を確認する。"""
from pathlib import Path
from urllib.parse import urlparse
import sys
import traceback
from playwright.sync_api import sync_playwright, expect


def main():
    root = Path(__file__).resolve().parent.parent
    settings = dict(line.split('=', 1) for line in (root / '.env').read_text().splitlines()
                    if line and not line.startswith('#') and '=' in line)
    base = settings['APP_BASE_URL']
    if urlparse(base).hostname != 'localhost' or urlparse(settings['OIDC_ISSUER']).hostname != 'localhost':
        raise RuntimeError('local URLs required')
    screenshots = root / '.cache' / 'verification'
    screenshots.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch()
        for username in ('alice', 'bob'):
            context = browser.new_context(viewport={'width': 1200, 'height': 800}, locale='ja-JP')
            page = context.new_page()
            page.goto(base + '/books')
            expect(page).to_have_url(base + '/')
            if username == 'alice':
                page.screenshot(path=str(screenshots / '03-home.png'))
            page.get_by_role('link', name='ログイン', exact=True).click()
            expect(page.locator('form#kc-form-login')).to_be_visible()
            if urlparse(page.url).netloc != urlparse(settings['OIDC_ISSUER']).netloc:
                raise RuntimeError('unexpected identity provider')
            if username == 'alice':
                page.screenshot(path=str(screenshots / '03-keycloak.png'))
            page.locator('input[name=username]').fill(username)
            page.locator('input[name=password]').fill(settings['LOCAL_USER_PASSWORD'])
            page.locator('form#kc-form-login').locator('input[type=submit],button[type=submit]').first.click()
            expect(page).to_have_url(base + '/books')
            expect(page.locator('.account span')).to_have_text(username)
            session = next(c for c in context.cookies() if c['name'] == 'readinglog_session')
            assert session['httpOnly'] and session['sameSite'] == 'Lax'
            if username == 'alice':
                page.screenshot(path=str(screenshots / '03-books.png'))
            page.set_viewport_size({'width': 390, 'height': 844})
            expect(page.get_by_role('button', name='ログアウト')).to_be_visible()
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
            if username == 'alice':
                page.screenshot(path=str(screenshots / '03-mobile.png'))
            page.get_by_role('button', name='ログアウト').click()
            expect(page).to_have_url(base + '/')
            assert not any(c['name'] == 'readinglog_session' for c in context.cookies())
            context.add_cookies([session])
            page.goto(base + '/books')
            expect(page).to_have_url(base + '/')
            context.close()
            print(f'{username}: browser login, mobile layout, logout, old-cookie rejection OK')
        browser.close()


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # ブラウザー例外のURLに認証コードが含まれるため、詳細をそのまま表示しない。
        line = traceback.extract_tb(error.__traceback__)[-1].lineno
        print(f'Browser verification failed ({type(error).__name__}, line {line}); check local application logs.', file=sys.stderr)
        sys.exit(1)
