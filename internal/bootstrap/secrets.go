// AWS SDKへの薄い接続部分。Secretの実値やSDKエラーをログへ返さない。
package bootstrap

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

type SecretsAPI interface {
	GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
	PutSecretValue(context.Context, *secretsmanager.PutSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error)
}

type AWSSecrets struct{ Client SecretsAPI }

func (s AWSSecrets) Read(ctx context.Context, arn string) (string, error) {
	out, err := s.Client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(arn), VersionStage: aws.String("AWSCURRENT")})
	if err != nil {
		var missing *types.ResourceNotFoundException
		if errors.As(err, &missing) {
			return "", ErrNoValue
		}
		return "", errors.New("get secret failed")
	}
	if out == nil || out.SecretString == nil || *out.SecretString == "" {
		return "", errors.New("secret must contain a nonempty string")
	}
	return *out.SecretString, nil
}

func (s AWSSecrets) Write(ctx context.Context, arn, value, token string) error {
	_, err := s.Client.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId: aws.String(arn), SecretString: aws.String(value), ClientRequestToken: aws.String(token), VersionStages: []string{"AWSCURRENT"},
	})
	if err != nil {
		return errors.New("put secret failed")
	}
	return nil
}
