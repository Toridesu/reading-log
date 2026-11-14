#!/usr/bin/env python3
"""実ブラウザーでCRUDと2ユーザー間の認可を確認する。作った検証用の本だけを最後に削除する。"""
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
        users = {}
        try:
            for username in ('alice', 'bob'):
                context = browser.new_context(viewport={'width': 1200, 'height': 900}, locale='ja-JP')
                page = context.new_page()
                users[username] = {'context': context, 'page': page, 'ids': []}
                page.goto(base + '/books')
                expect(page).to_have_url(base + '/')
                page.get_by_role('link', name='ログイン', exact=True).click()
                expect(page.locator('form#kc-form-login')).to_be_visible()
                if urlparse(page.url).netloc != urlparse(settings['OIDC_ISSUER']).netloc:
                    raise RuntimeError('unexpected identity provider')
                page.locator('input[name=username]').fill(username)
                page.locator('input[name=password]').fill(settings['LOCAL_USER_PASSWORD'])
                page.locator('form#kc-form-login').locator('input[type=submit],button[type=submit]').first.click()
                expect(page).to_have_url(base + '/books')
                expect(page.locator('.account span')).to_have_text(username)
                users[username]['csrf'] = page.locator('.account input[name=csrf_token]').input_value()

            def create(username, title, status, notes):
                user = users[username]
                page = user['page']
                page.goto(base + '/books/new')
                page.get_by_label('書名', exact=True).fill(title)
                page.get_by_label('状態', exact=True).select_option(status)
                page.get_by_label('メモ', exact=True).fill(notes)
                with page.expect_navigation():
                    page.get_by_role('button', name='追加', exact=True).click()
                book_id = int(page.url.rsplit('/', 1)[1])
                user['ids'].append(book_id)
                expect(page.locator('.book-detail h2')).to_have_text(title)
                return book_id

            def post(username, path, fields, origin=base):
                return users[username]['context'].request.post(
                    base + path, form=fields, headers={'Origin': origin}, max_redirects=0)

            alice = users['alice']['page']
            bob = users['bob']['page']
            book_id = create('alice', 'クラウド設計の読書ノート', 'reading', '第1章を読んだ。\n通信経路を図にして整理する。')
            create('alice', 'Goで学ぶWebアプリ開発', 'unread', '')
            create('alice', 'PostgreSQLのしくみ', 'finished', 'バックアップと復元を試す。')
            alice.goto(base + '/books')
            expect(alice.locator('.book-list')).to_contain_text('クラウド設計の読書ノート')
            for label in ('未読', '読書中', '読了'):
                expect(alice.locator('.book-list')).to_contain_text(label)
            alice.screenshot(path=str(screenshots / '04-books.png'), full_page=True)
            alice.set_viewport_size({'width': 390, 'height': 844})
            assert alice.evaluate('document.documentElement.scrollWidth <= innerWidth')
            alice.screenshot(path=str(screenshots / '04-mobile.png'), full_page=True)

            alice.goto(base + f'/books/{book_id}')
            expect(alice.locator('.notes')).to_have_text('第1章を読んだ。\n通信経路を図にして整理する。')
            alice.get_by_role('link', name='編集', exact=True).click()
            expect(alice.get_by_label('書名', exact=True)).to_have_value('クラウド設計の読書ノート')
            assert alice.evaluate('document.documentElement.scrollWidth <= innerWidth')
            alice.get_by_label('書名', exact=True).fill('クラウド設計と運用')
            alice.get_by_label('状態', exact=True).select_option('finished')
            alice.get_by_label('メモ', exact=True).fill('読み終えた。\n次は自分の環境で試す。')
            alice.set_viewport_size({'width': 1200, 'height': 900})
            alice.screenshot(path=str(screenshots / '04-form.png'), full_page=True)
            alice.get_by_role('button', name='保存', exact=True).click()
            expect(alice.locator('.book-detail h2')).to_have_text('クラウド設計と運用')
            expect(alice.locator('.status')).to_have_text('読了')
            expect(alice.locator('.notes')).to_have_text('読み終えた。\n次は自分の環境で試す。')
            alice.screenshot(path=str(screenshots / '04-detail.png'), full_page=True)

            # Bobの正しいセッション・CSRFでもAliceの本は参照・更新・削除できない。
            bob.goto(base + '/books')
            expect(bob.locator('main')).not_to_contain_text('クラウド設計と運用')
            bob_id = create('bob', 'Bobの検証用ノート', 'unread', 'Bobだけが操作できる。')
            for suffix in ('', '/edit', '/delete'):
                response = users['bob']['context'].request.get(base + f'/books/{book_id}' + suffix)
                assert response.status == 404
                assert 'クラウド設計と運用' not in response.text()
            fields = {'csrf_token': users['bob']['csrf'], 'title': 'tampered', 'status': 'unread', 'notes': ''}
            assert post('bob', f'/books/{book_id}', fields).status == 404
            assert post('bob', f'/books/{book_id}/delete', {'csrf_token': users['bob']['csrf']}).status == 404
            assert users['alice']['context'].request.get(base + f'/books/{bob_id}').status == 404
            fields['user_id'] = '1'
            assert post('bob', '/books', fields).status == 400
            del fields['user_id']
            assert post('bob', '/books?user_id=1', fields).status == 400
            fields['csrf_token'] = 'incorrect'
            assert post('bob', '/books', fields).status == 403
            fields['csrf_token'] = users['bob']['csrf']
            assert post('bob', '/books', fields, origin='https://example.invalid').status == 403
            fields['title'] = ' '
            response = post('bob', '/books', fields)
            assert response.status == 422 and '書名は1〜300文字で入力してください' in response.text()
            alice.reload()
            expect(alice.locator('.book-detail h2')).to_have_text('クラウド設計と運用')
            expect(alice.locator('.notes')).to_have_text('読み終えた。\n次は自分の環境で試す。')
            print('PASS: real sessions, CRUD, ownership checks, tampering, CSRF, validation, mobile layout')

            alice.get_by_role('link', name='削除', exact=True).click()
            alice.screenshot(path=str(screenshots / '04-delete.png'), full_page=True)
            alice.get_by_role('link', name='キャンセル', exact=True).click()
            expect(alice.locator('.book-detail h2')).to_have_text('クラウド設計と運用')
            alice.get_by_role('link', name='削除', exact=True).click()
            alice.get_by_role('button', name='削除する', exact=True).click()
            expect(alice).to_have_url(base + '/books')
            assert users['alice']['context'].request.get(base + f'/books/{book_id}').status == 404
            users['alice']['ids'].remove(book_id)
            print('PASS: deletion requires confirmation POST; deleted book returns 404')
        finally:
            # 既存データには触れず、この実行で返されたIDだけを所有者のセッションで削除する。
            for user in users.values():
                for book_id in user['ids']:
                    response = user['context'].request.post(base + f'/books/{book_id}/delete',
                        form={'csrf_token': user['csrf']}, headers={'Origin': base}, max_redirects=0)
                    assert response.status in (303, 404), 'test-book cleanup failed'
                if 'csrf' in user:
                    response = user['context'].request.post(base + '/auth/logout',
                        form={'csrf_token': user['csrf']}, headers={'Origin': base}, max_redirects=0)
                    assert response.status == 303, 'test-session logout failed'
                user['context'].close()
            browser.close()
        print('PASS: test books removed and sessions logged out')


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # 認証コード・Cookie・パスワードが例外に含まれる可能性があるため詳細は出さない。
        line = traceback.extract_tb(error.__traceback__)[-1].lineno
        print(f'Book verification failed ({type(error).__name__}, line {line}); check local application logs.', file=sys.stderr)
        sys.exit(1)
