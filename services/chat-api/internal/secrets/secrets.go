// Package secrets loads runtime secrets from AWS Secrets Manager.
package secrets

import (
	"context"
	"encoding/json"
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
	return keyFromSecret(secretID, strings.TrimSpace(*out.SecretString))
}

// keyFromSecret extracts the API key from a secret stored either as the bare
// key or as a JSON object. The Secrets Manager console's "Key/value" editor
// stores secrets as JSON (e.g. {"ANTHROPIC_API_KEY":"sk-..."}), while
// `put-secret-value --secret-string sk-...` stores the bare string; a deploy
// must not break just because the key was entered through the console. A raw
// Anthropic key is never valid JSON, so anything not starting with '{' is
// treated as the key verbatim.
func keyFromSecret(secretID, s string) (string, error) {
	if !strings.HasPrefix(s, "{") {
		return s, nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return "", fmt.Errorf("secrets: %q looks like JSON but is not a flat {\"key\":\"value\"} object: %w; store it as a plaintext key or a JSON object with a string value", secretID, err)
	}
	// Prefer a recognized field name, then fall back to the sole entry so a
	// single-key object works whatever the field is called.
	for _, k := range []string{"ANTHROPIC_API_KEY", "anthropic_api_key", "api_key", "key"} {
		if v := strings.TrimSpace(m[k]); v != "" {
			return v, nil
		}
	}
	if len(m) == 1 {
		for _, v := range m {
			if v := strings.TrimSpace(v); v != "" {
				return v, nil
			}
		}
	}
	return "", fmt.Errorf("secrets: %q is a JSON object with no non-empty API-key field (want one of ANTHROPIC_API_KEY, api_key, key, or a single entry)", secretID)
}

// Client builds a Secrets Manager client from the ambient credential chain.
func Client(ctx context.Context) (*secretsmanager.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("secrets: load aws config: %w", err)
	}
	return secretsmanager.NewFromConfig(cfg), nil
}
