// Webアプリの入口。DBにつなぎ、HTTPサーバーを起動・停止する。
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

	"readinglog/internal/database"
	"readinglog/internal/web"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server_failed", "error", err.Error())
		os.Exit(1)
	}
}

// run はHTTPサーバーの起動から終了までを担当する。
func run(logger *slog.Logger) error {
	addr := os.Getenv("APP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	// 起動前にDBへの接続を確かめる。接続できなければ最大5秒で打ち切る。
	connectCtx, cancelConnect := context.WithTimeout(context.Background(), 5*time.Second)
	db, err := database.Open(connectCtx, os.Getenv("DATABASE_URL"))
	cancelConnect()
	if err != nil {
		return err
	}
	defer db.Close()
	logger.Info("database_connected")

	handler, err := web.NewHandler(logger, db.PingContext)
	if err != nil {
		return fmt.Errorf("prepare HTTP handler: %w", err)
	}

	// 遅い接続を無期限に待たないよう、タイムアウトを設定する。
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Ctrl+Cやコンテナの停止通知を受け取る。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	// HTTP受付を並行実行し、サーバーの終了結果をchannelで受け取る。
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- server.Serve(listener) }()
	logger.Info("server_started", "addr", listener.Addr().String())

	// サーバーの終了か停止通知を待つ。
	select {
	case err := <-errorsCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	logger.Info("server_stopping")
	// 停止通知用ctxはキャンセル済みなので、新しいContextで処理中の応答を最大10秒待つ。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("server_stopped")
	return nil
}
