// package main は、実行できるプログラムを作るためのパッケージ名。
// 実行すると、このファイルの main 関数から処理が始まる。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// go.mod の module 名「readinglog」に続けて、プロジェクト内のパッケージを指定する。
	"readinglog/internal/web"
)

// main は起動の入口。処理本体は run に分け、失敗をここでログに残す。
func main() {
	// := は変数の宣言と代入を同時に行う書き方。型は右辺から推論される。
	// 標準出力へJSON形式でログを出す。nil は、ここでは追加オプションなしの意味。
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// Goでは失敗をerror型の戻り値で受け取り、nil（エラーなし）か確認する。
	// この書き方のerrは、ifの条件とブロックの中だけで使える。
	if err := run(logger); err != nil {
		logger.Error("server_failed", "error", err.Error())
		// 0以外の終了コードで、シェルやコンテナに失敗を伝える。
		os.Exit(1)
	}
}

// run はHTTPサーバーの起動から終了までを担当し、失敗した場合はerrorを返す。
// *slog.Logger はLoggerへのポインタ。同じロガーを各処理へ渡して使う。
func run(logger *slog.Logger) error {
	// 環境変数がなければ、このPCからアクセスするためのアドレスを使う。
	addr := os.Getenv("APP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	// Goの関数は複数の値を返せる。ここでは「HTTP処理」と「作成時のエラー」。
	// HTMLの準備などが失敗したら、ポートを開く前に起動を中止する。
	handler, err := web.NewHandler(logger)
	if err != nil {
		// %w は元のエラーを包み、原因を調べられる形のまま説明を付ける。
		return fmt.Errorf("prepare HTTP handler: %w", err)
	}

	// 構造体（struct）の各フィールドに設定を入れる。& はその値へのポインタを得る。
	// タイムアウトを設け、遅い接続を無期限に待たないようにする。
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Contextは処理のキャンセルや期限を伝える仕組み。
	// Ctrl+C（Interrupt）やコンテナ停止時のSIGTERMを受けるとctxがキャンセルされる。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// defer は、この関数を抜けるときに実行する後片付けを登録する。
	defer stop()

	// 実際にポートを開く。同じポートが使用中なら、ここでエラーになる。
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	// channelは並行して動く処理どうしが値を受け渡す通路。
	// 容量1にすることで、受信側がまだ待っていなくても終了結果を1つ送れる。
	errorsCh := make(chan error, 1)
	// go を付けた関数はgoroutine（Goの並行処理）として実行される。
	// ServeはHTTPリクエストを受け付け続け、終了したら結果をchannelへ送る。
	go func() { errorsCh <- server.Serve(listener) }()
	logger.Info("server_started", "addr", listener.Addr().String())

	// selectは複数のchannelを待ち、準備ができたcaseを実行する。
	// ここでは「サーバーの終了」か「停止シグナル」のどちらかを待つ。
	select {
	case err := <-errorsCh:
		// Shutdown等による通常の終了は、アプリの障害として扱わない。
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		// 停止要求を受けたので、selectを抜けて下の終了処理へ進む。
	}

	logger.Info("server_stopping")
	// 停止通知用ctxは既にキャンセル済みなので、新しいContextで最大10秒待つ。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// 新しい接続の受付を止め、処理中のリクエストが終わるのを待つ。
	if err := server.Shutdown(shutdownCtx); err != nil {
		// 待ち時間を超えた場合などは接続を閉じる。_ は戻り値を使わないという指定。
		_ = server.Close()
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("server_stopped")
	return nil
}
