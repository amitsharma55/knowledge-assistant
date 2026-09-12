package embed

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// invoker is the slice of the Bedrock Runtime client this package uses. A
// narrow interface keeps the SDK out of tests, which stub it.
type invoker interface {
	InvokeModel(ctx context.Context, in *bedrockruntime.InvokeModelInput, opts ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
}

// Bedrock embeds text with Amazon Titan Text Embeddings v2. Dim is the width
// the caller expects and is enforced on every response, for the same reason
// the Ollama embedder enforces it: the index mapping fixes the width, and a
// mismatch otherwise surfaces far from its cause.
type Bedrock struct {
	Model string
	Dim   int
	api   invoker
}

func (b Bedrock) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(map[string]any{
		"inputText":  text,
		"dimensions": b.Dim,
		"normalize":  true,
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock embed: encode request: %w", err)
	}
	out, err := b.api.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(b.Model),
		Body:        body,
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock embed: invoke %s: %w", b.Model, err)
	}
	var resp struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.Unmarshal(out.Body, &resp); err != nil {
		return nil, fmt.Errorf("bedrock embed: decode response: %w", err)
	}
	if b.Dim > 0 && len(resp.Embedding) != b.Dim {
		return nil, fmt.Errorf(
			"bedrock embed: model %q returned a %d-dimension vector but %d was configured; "+
				"set KA_EMBED_DIM to match KA_EMBED_MODEL, then delete and reindex",
			b.Model, len(resp.Embedding), b.Dim)
	}
	return resp.Embedding, nil
}
