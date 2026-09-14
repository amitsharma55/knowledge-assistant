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

func TestFetchAPIKeyJSON(t *testing.T) {
	cases := map[string]string{
		"named field":        `{"ANTHROPIC_API_KEY":"sk-json"}`,
		"lowercase api_key":  `{"api_key":"sk-json"}`,
		"single unknown key": `{"whatever":"sk-json"}`,
		"padded json":        "  " + `{"ANTHROPIC_API_KEY":"sk-json"}` + "\n",
	}
	for name, val := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := FetchAPIKey(context.Background(), stubSM{val: val}, "ka/anthropic-api-key")
			if err != nil {
				t.Fatalf("FetchAPIKey: %v", err)
			}
			if got != "sk-json" {
				t.Fatalf("got %q, want sk-json", got)
			}
		})
	}
}

func TestFetchAPIKeyJSONErrors(t *testing.T) {
	// A JSON object with no usable field, and malformed JSON, must fail loudly
	// rather than hand a broken key to the Anthropic client.
	for name, val := range map[string]string{
		"no usable field": `{"a":"","b":""}`,
		"malformed":       `{"ANTHROPIC_API_KEY":`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := FetchAPIKey(context.Background(), stubSM{val: val}, "id"); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
