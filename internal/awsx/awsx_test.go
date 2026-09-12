package awsx

import (
	"context"
	"testing"
)

func TestBedrockRuntimeUsesRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	// No credentials needed: the client is built lazily and signs only on a call.
	c, err := BedrockRuntime(context.Background())
	if err != nil {
		t.Fatalf("BedrockRuntime: %v", err)
	}
	if c == nil {
		t.Fatal("BedrockRuntime returned a nil client")
	}
}
