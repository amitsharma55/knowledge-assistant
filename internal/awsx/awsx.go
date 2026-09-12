// Package awsx builds AWS SDK clients from the ambient credential chain.
//
// In EKS, Pod Identity exposes credentials through the container-credentials
// endpoint that config.LoadDefaultConfig reads automatically, so there is no
// credential handling here and no static keys anywhere.
package awsx

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// BedrockRuntime returns a Bedrock Runtime client. The region comes from the
// environment (AWS_REGION); calls fail fast if it is unset.
func BedrockRuntime(ctx context.Context) (*bedrockruntime.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("awsx: load aws config: %w", err)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("awsx: no AWS region; set AWS_REGION")
	}
	return bedrockruntime.NewFromConfig(cfg), nil
}
