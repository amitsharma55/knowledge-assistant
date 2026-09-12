package embed

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

type stubInvoker struct {
	gotBody []byte
	out     []byte
	err     error
}

func (s *stubInvoker) InvokeModel(_ context.Context, in *bedrockruntime.InvokeModelInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
	s.gotBody = in.Body
	if s.err != nil {
		return nil, s.err
	}
	return &bedrockruntime.InvokeModelOutput{Body: s.out}, nil
}

func TestBedrockEmbedRequestAndParse(t *testing.T) {
	stub := &stubInvoker{out: []byte(`{"embedding":[0.1,0.2,0.3],"inputTextTokenCount":2}`)}
	e := Bedrock{Model: "amazon.titan-embed-text-v2:0", Dim: 3, api: stub}

	v, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 3 || v[0] != 0.1 {
		t.Fatalf("unexpected vector %v", v)
	}

	var req struct {
		InputText  string `json:"inputText"`
		Dimensions int    `json:"dimensions"`
		Normalize  bool   `json:"normalize"`
	}
	if err := json.Unmarshal(stub.gotBody, &req); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if req.InputText != "hello world" || req.Dimensions != 3 || !req.Normalize {
		t.Fatalf("unexpected request %+v", req)
	}
}

func TestBedrockEmbedDimMismatch(t *testing.T) {
	stub := &stubInvoker{out: []byte(`{"embedding":[0.1,0.2]}`)}
	e := Bedrock{Model: "amazon.titan-embed-text-v2:0", Dim: 3, api: stub}
	if _, err := e.Embed(context.Background(), "x"); err == nil {
		t.Fatal("expected a dimension-mismatch error")
	}
}
