// Package secrets loads runtime secrets from AWS Secrets Manager.
package secrets

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type secretGetter interface {
	GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput, opts ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// FetchAPIKey returns the plaintext secret value for secretID.
func FetchAPIKey(ctx context.Context, api secretGetter, secretID string) (string, error) {
	out, err := api.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(secretID)})
	if err != nil {
		return "", fmt.Errorf("secrets: get %q: %w", secretID, err)
	}
	if out.SecretString == nil || strings.TrimSpace(*out.SecretString) == "" {
		return "", fmt.Errorf("secrets: %q has no string value", secretID)
	}
	return strings.TrimSpace(*out.SecretString), nil
}

// Client builds a Secrets Manager client from the ambient credential chain.
func Client(ctx context.Context) (*secretsmanager.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("secrets: load aws config: %w", err)
	}
	return secretsmanager.NewFromConfig(cfg), nil
}
