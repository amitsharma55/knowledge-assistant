package classify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Ollama classifies with a chat model served by a local Ollama instance. It
// mirrors rewrite.Ollama's request shape.
type Ollama struct {
	BaseURL string
	Model   string
	HTTP    *http.Client
}

var boolSchema = map[string]any{
	"type":       "object",
	"properties": map[string]any{"ticket": map[string]any{"type": "boolean"}},
	"required":   []string{"ticket"},
}

func (o Ollama) client() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return http.DefaultClient
}

func (o Ollama) Classify(ctx context.Context, question string) (bool, error) {
	payload, err := json.Marshal(map[string]any{
		"model": o.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": question},
		},
		"stream":  false,
		"format":  boolSchema,
		"options": map[string]any{"temperature": 0},
	})
	if err != nil {
		return false, fmt.Errorf("classify: encode request: %w", err)
	}
	url := strings.TrimSuffix(o.BaseURL, "/") + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return false, fmt.Errorf("classify: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client().Do(req)
	if err != nil {
		return false, fmt.Errorf("classify: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return false, fmt.Errorf("classify: %s returned %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, fmt.Errorf("classify: decode response: %w", err)
	}
	return parseTicketBool(out.Message.Content)
}
