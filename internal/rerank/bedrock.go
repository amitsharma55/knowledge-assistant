package rerank

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

// Bedrock reranks with gpt-oss-20b on Bedrock via the Converse API. It shares
// the prompt and the tolerant parser with the Ollama backend; only the call
// differs.
type Bedrock struct {
	Model string
	api   converser
}

// NewBedrock builds a Bedrock reranker. The client is taken as the narrow
// converser interface so callers in tests can substitute a stub.
func NewBedrock(model string, api converser) Bedrock { return Bedrock{Model: model, api: api} }

func (b Bedrock) Rerank(ctx context.Context, query string, chunks []rag.Chunk) ([]rag.Chunk, error) {
	// One chunk cannot be reordered, and zero has nothing to send.
	if len(chunks) < 2 {
		return chunks, nil
	}
	out, err := b.api.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId: aws.String(b.Model),
		System:  []brtypes.SystemContentBlock{&brtypes.SystemContentBlockMemberText{Value: systemPrompt}},
		Messages: []brtypes.Message{{
			Role:    brtypes.ConversationRoleUser,
			Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: buildRerankPrompt(query, chunks)}},
		}},
		InferenceConfig: &brtypes.InferenceConfiguration{Temperature: aws.Float32(0)},
	})
	if err != nil {
		return nil, fmt.Errorf("rerank: bedrock converse %s: %w", b.Model, err)
	}
	order, err := parseOrder(converseText(out))
	if err != nil {
		return nil, err
	}
	return Reorder(chunks, order), nil
}

// converseText concatenates the assistant message's text blocks, skipping any
// reasoning-only blocks a model such as gpt-oss emits first.
func converseText(out *bedrockruntime.ConverseOutput) string {
	msg, ok := out.Output.(*brtypes.ConverseOutputMemberMessage)
	if !ok {
		return ""
	}
	var s string
	for _, block := range msg.Value.Content {
		if t, ok := block.(*brtypes.ContentBlockMemberText); ok {
			s += t.Value
		}
	}
	return s
}
