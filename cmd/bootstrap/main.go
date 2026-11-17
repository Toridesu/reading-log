// AWS展開時に一度実行するDB初期化コマンド。Webの起動処理には組み込まない。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"readinglog/internal/bootstrap"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("database bootstrap completed; application connection verified")
}

func run() error {
	// 明示的なサブコマンドがない実行は、認証情報の取得前に止める。
	if len(os.Args) != 2 || os.Args[1] != "initialize" {
		return errors.New("usage: bootstrap initialize")
	}
	c := bootstrap.Config{Host: os.Getenv("DB_HOST"), Port: "5432", RootCert: bootstrap.CAFile,
		MasterSecretARN: os.Getenv("MASTER_SECRET_ARN"), ApplicationSecretARN: os.Getenv("APP_SECRET_ARN")}
	if err := c.Validate(); err != nil {
		return err
	}
	for _, id := range []string{c.MasterSecretARN, c.ApplicationSecretARN} {
		a, err := arn.Parse(id)
		if err != nil || a.Partition != "aws" || a.Service != "secretsmanager" || a.Region != "ap-northeast-1" || a.AccountID == "" || a.Resource == "" {
			return errors.New("bootstrap requires Tokyo Secrets Manager ARNs")
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("ap-northeast-1"))
	if err != nil {
		return errors.New("load AWS configuration failed")
	}
	cfg.ClientLogMode = 0
	return bootstrap.Run(ctx, c, bootstrap.AWSSecrets{Client: secretsmanager.NewFromConfig(cfg)})
}
