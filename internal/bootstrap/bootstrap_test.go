// 秘密情報の取り違え・エラーへの混入と、AWS APIに渡す内容をDBなしで確認する。
package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

func TestApplicationSecretValidation(t *testing.T) {
	c := Config{Host: "db.example", Port: "5432", RootCert: CAFile, MasterSecretARN: "master", ApplicationSecretARN: "app"}
	password := `quotes'and\slashes:@/?#%日本語`
	raw := c.connectionURL("readinglog", password)
	got, err := c.applicationPassword(raw)
	if err != nil || got != password {
		t.Fatal("password did not round trip")
	}
	for _, input := range []string{
		c.connectionURL("readinglog_admin", password),
		strings.Replace(raw, "db.example", "other.example", 1),
		strings.Replace(raw, "verify-full", "disable", 1),
		raw + "&search_path=other", "invalid-secret-password", c.connectionURL("readinglog", ""),
	} {
		if _, err := c.applicationPassword(input); err == nil || strings.Contains(err.Error(), password) || strings.Contains(err.Error(), "invalid-secret-password") {
			t.Fatal("invalid secret was accepted or leaked")
		}
	}
}

type fakeAPI struct {
	get   *secretsmanager.GetSecretValueInput
	put   *secretsmanager.PutSecretValueInput
	value *secretsmanager.GetSecretValueOutput
	err   error
}

func (f *fakeAPI) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	f.get = in
	return f.value, f.err
}
func (f *fakeAPI) PutSecretValue(_ context.Context, in *secretsmanager.PutSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error) {
	f.put = in
	return &secretsmanager.PutSecretValueOutput{}, f.err
}

func TestAWSSecretBoundary(t *testing.T) {
	ctx := context.Background()
	api := &fakeAPI{value: &secretsmanager.GetSecretValueOutput{SecretString: aws.String("secret-password")}}
	s := AWSSecrets{Client: api}
	value, err := s.Read(ctx, "app-arn")
	if err != nil || value != "secret-password" || aws.ToString(api.get.SecretId) != "app-arn" || aws.ToString(api.get.VersionStage) != "AWSCURRENT" {
		t.Fatal("read request mismatch")
	}
	token := strings.Repeat("a", 64)
	if err := s.Write(ctx, "app-arn", value, token); err != nil || aws.ToString(api.put.SecretId) != "app-arn" || aws.ToString(api.put.SecretString) != value || aws.ToString(api.put.ClientRequestToken) != token || len(api.put.VersionStages) != 1 || api.put.VersionStages[0] != "AWSCURRENT" {
		t.Fatal("write request mismatch")
	}
	for _, err := range []error{errors.New("secret-password"), &types.ResourceNotFoundException{Message: aws.String("secret-password")}} {
		api.err = err
		_, got := s.Read(ctx, "app-arn")
		if got == nil || strings.Contains(got.Error(), "secret-password") {
			t.Fatal("SDK error leaked")
		}
		var missing *types.ResourceNotFoundException
		if errors.Is(got, ErrNoValue) != errors.As(err, &missing) {
			t.Fatal("only ResourceNotFound may be treated as missing")
		}
		if err := s.Write(ctx, "app-arn", value, token); err == nil || strings.Contains(err.Error(), value) {
			t.Fatal("write error leaked")
		}
	}
	api.err = nil
	for _, out := range []*secretsmanager.GetSecretValueOutput{nil, {}, {SecretString: aws.String("")}} {
		api.value = out
		if _, err := s.Read(ctx, "app-arn"); err == nil {
			t.Fatal("empty value accepted")
		}
	}
}
