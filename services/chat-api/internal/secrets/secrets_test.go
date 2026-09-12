package secrets

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type stubSM struct {
	val string
	err error
}

func (s stubSM) GetSecretValue(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(s.val)}, nil
}

func TestFetchAPIKey(t *testing.T) {
	got, err := FetchAPIKey(context.Background(), stubSM{val: "sk-test"}, "ka/anthropic-api-key")
	if err != nil {
		t.Fatalf("FetchAPIKey: %v", err)
	}
	if got != "sk-test" {
		t.Fatalf("got %q", got)
	}
}

func TestFetchAPIKeyError(t *testing.T) {
	if _, err := FetchAPIKey(context.Background(), stubSM{err: errors.New("denied")}, "id"); err == nil {
		t.Fatal("expected error")
	}
}
