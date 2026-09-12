package rerank

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/example/knowledge-assistant/internal/rag"
)

type stubConverser struct {
	in   *bedrockruntime.ConverseInput
	text string
}

func (s *stubConverser) Converse(_ context.Context, in *bedrockruntime.ConverseInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	s.in = in
	return &bedrockruntime.ConverseOutput{
		Output: &brtypes.ConverseOutputMemberMessage{
			Value: brtypes.Message{
				Role:    brtypes.ConversationRoleAssistant,
				Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: s.text}},
			},
		},
	}, nil
}

func TestBedrockRerankReordersAndSendsSystem(t *testing.T) {
	stub := &stubConverser{text: `thinking... {"order":[3,1,2]}`}
	r := Bedrock{Model: "openai.gpt-oss-20b-1:0", api: stub}
	chunks := []rag.Chunk{{Text: "a"}, {Text: "b"}, {Text: "c"}}

	got, err := r.Rerank(context.Background(), "q?", chunks)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if got[0].Text != "c" {
		t.Fatalf("expected chunk c first, got %q", got[0].Text)
	}
	if aws.ToString(stub.in.ModelId) != "openai.gpt-oss-20b-1:0" {
		t.Fatalf("wrong model id %q", aws.ToString(stub.in.ModelId))
	}
	if len(stub.in.System) == 0 {
		t.Fatal("system prompt not sent")
	}
}

func TestBedrockRerankShortCircuits(t *testing.T) {
	stub := &stubConverser{text: "should not be called"}
	r := Bedrock{Model: "m", api: stub}
	one := []rag.Chunk{{Text: "a"}}
	got, err := r.Rerank(context.Background(), "q", one)
	if err != nil || len(got) != 1 {
		t.Fatalf("short-circuit failed: %v %v", got, err)
	}
	if stub.in != nil {
		t.Fatal("model was called for a single chunk")
	}
}
