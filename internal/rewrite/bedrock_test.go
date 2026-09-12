package rewrite

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
			Value: brtypes.Message{Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: s.text}}},
		},
	}, nil
}

func TestBedrockRewrite(t *testing.T) {
	stub := &stubConverser{text: `{"query":"AVR field mapping"}`}
	r := NewBedrock("openai.gpt-oss-20b-1:0", 6, stub)
	got, err := r.Rewrite(context.Background(), "I mean AVR", []rag.Turn{{Role: "user", Content: "fields?"}})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if got != "AVR field mapping" {
		t.Fatalf("got %q", got)
	}
	if aws.ToString(stub.in.ModelId) != "openai.gpt-oss-20b-1:0" {
		t.Fatalf("wrong model %q", aws.ToString(stub.in.ModelId))
	}
}
