package classify

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

type converser interface {
	Converse(ctx context.Context, in *bedrockruntime.ConverseInput, opts ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

// Bedrock classifies ticket intent with a Converse model (e.g. gpt-oss-20b).
type Bedrock struct {
	Model string
	api   converser
}

func NewBedrock(model string, api converser) Bedrock { return Bedrock{Model: model, api: api} }

func (b Bedrock) Classify(ctx context.Context, question string) (bool, error) {
	out, err := b.api.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId: aws.String(b.Model),
		System:  []brtypes.SystemContentBlock{&brtypes.SystemContentBlockMemberText{Value: systemPrompt}},
		Messages: []brtypes.Message{{
			Role:    brtypes.ConversationRoleUser,
			Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: question}},
		}},
		InferenceConfig: &brtypes.InferenceConfiguration{Temperature: aws.Float32(0)},
	})
	if err != nil {
		return false, fmt.Errorf("classify: bedrock converse %s: %w", b.Model, err)
	}
	msg, ok := out.Output.(*brtypes.ConverseOutputMemberMessage)
	if !ok {
		return false, fmt.Errorf("classify: bedrock returned no message")
	}
	var text string
	for _, block := range msg.Value.Content {
		if t, ok := block.(*brtypes.ContentBlockMemberText); ok {
			text += t.Value
		}
	}
	return parseTicketBool(text)
}
