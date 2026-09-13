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

func TestS3UsesRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	c, err := S3(context.Background())
	if err != nil {
		t.Fatalf("S3: %v", err)
	}
	if c == nil {
		t.Fatal("S3 returned a nil client")
	}
}
