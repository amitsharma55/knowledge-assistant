package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Ollama embeds text with a model served by a local Ollama instance. It is
// the local-dev and demo embedder: no API key, no per-token cost, and the
// model runs in the container brought up by `make dev-up`.
//
// Dim is the width the caller expects, and is enforced on every response.
// The index mapping fixes the vector width at creation time, so a model
// that returns some other width can only produce write failures far from
// the cause -- see Embed.
type Ollama struct {
	BaseURL string
	Model   string
	Dim     int
	HTTP    *http.Client
}

func (o Ollama) client() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return http.DefaultClient
}

func (o Ollama) Embed(ctx context.Context, text string) ([]float32, error) {
	payload, err := json.Marshal(map[string]any{"model": o.Model, "input": text})
	if err != nil {
		return nil, fmt.Errorf("ollama embed: encode request: %w", err)
	}
	url := strings.TrimSuffix(o.BaseURL, "/") + "/api/embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		// Ollama reports an unpulled model as a 404 with a JSON body naming
		// it. That is the most common setup mistake here, so pass the
		// server's own words through rather than just the status code.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("ollama embed: %s returned %d: %s",
			url, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ollama embed: decode response: %w", err)
	}
	if len(out.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama embed: model %q returned no embeddings", o.Model)
	}
	v := out.Embeddings[0]
	if o.Dim > 0 && len(v) != o.Dim {
		// Caught here on purpose. An index created for Dim rejects a vector
		// of any other width, and OpenSearch's message names neither the
		// model nor the config that chose it.
		return nil, fmt.Errorf(
			"ollama embed: model %q returned a %d-dimension vector but %d was configured; "+
				"set KA_EMBED_DIM to match KA_EMBED_MODEL, then delete and reindex",
			o.Model, len(v), o.Dim)
	}
	return v, nil
}
