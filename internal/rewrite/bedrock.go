package rewrite

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/example/knowledge-assistant/internal/rag"
)

type converser interface {
	Converse(ctx context.Context, in *bedrockruntime.ConverseInput, opts ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

// Bedrock rewrites follow-up questions with gpt-oss-20b on Bedrock via Converse.
type Bedrock struct {
	Model    string
	MaxTurns int
	api      converser
}

func NewBedrock(model string, maxTurns int, api converser) Bedrock {
	return Bedrock{Model: model, MaxTurns: maxTurns, api: api}
}

func (b Bedrock) Rewrite(ctx context.Context, question string, history []rag.Turn) (string, error) {
	out, err := b.api.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId: aws.String(b.Model),
		System:  []brtypes.SystemContentBlock{&brtypes.SystemContentBlockMemberText{Value: systemPrompt}},
		Messages: []brtypes.Message{{
			Role:    brtypes.ConversationRoleUser,
			Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: buildRewritePrompt(question, history, b.MaxTurns)}},
		}},
		InferenceConfig: &brtypes.InferenceConfiguration{Temperature: aws.Float32(0)},
	})
	if err != nil {
		return "", fmt.Errorf("rewrite: bedrock converse %s: %w", b.Model, err)
	}
	msg, ok := out.Output.(*brtypes.ConverseOutputMemberMessage)
	if !ok {
		return "", fmt.Errorf("rewrite: bedrock returned no message")
	}
	var text string
	for _, block := range msg.Value.Content {
		if t, ok := block.(*brtypes.ContentBlockMemberText); ok {
			text += t.Value
		}
	}
	return parseQuery(text)
}
