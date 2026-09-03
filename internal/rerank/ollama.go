package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/example/knowledge-assistant/internal/rag"
)

// Ollama reranks with a chat model served by a local Ollama instance.
//
// It asks for a JSON schema rather than a plain "reply with JSON" instruction.
// gpt-oss is a reasoning model: it emits its thinking first, so an
// unconstrained reply begins "We need to rank the chunks by relevance..." and
// no JSON parser will touch it. Ollama's structured-output `format` field
// constrains the decode itself, which is the only reliable way to get the
// ranking back.
type Ollama struct {
	BaseURL string
	Model   string
	HTTP    *http.Client
}

const systemPrompt = `You are a document re-ranker.
You are given a question and a numbered list of text chunks retrieved from a knowledge base.
The chunks are in the order retrieval returned them, which is approximately by relevance; you can usually improve on it.
Rank every chunk by how well it answers the question, most relevant first.
Judge whether a chunk contains the specific facts the question asks for, not merely whether it covers the same topic.
Reply only with the ranked chunk ids. Include every id you were given, exactly once.`

// orderSchema constrains the reply to {"order": [ints]}.
var orderSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"order": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "integer"},
		},
	},
	"required": []string{"order"},
}

func (o Ollama) client() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return http.DefaultClient
}

// Rerank returns chunks ordered by the model's judgement. The returned slice
// always holds exactly the input chunks -- see Reorder.
func (o Ollama) Rerank(ctx context.Context, query string, chunks []rag.Chunk) ([]rag.Chunk, error) {
	// One chunk cannot be reordered, and zero has nothing to send. Skipping
	// the call keeps a trivial query off the model entirely.
	if len(chunks) < 2 {
		return chunks, nil
	}

	var u strings.Builder
	fmt.Fprintf(&u, "Question:\n\n%s\n\nChunks:\n\n", query)
	for i, c := range chunks {
		fmt.Fprintf(&u, "# CHUNK ID: %d\nsource: %s — %s\n\n%s\n\n", i+1, c.PageTitle, c.SectionPath, c.Text)
	}
	fmt.Fprintf(&u, "Rank all %d chunk ids by relevance to the question, most relevant first.", len(chunks))

	payload, err := json.Marshal(map[string]any{
		"model": o.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": u.String()},
		},
		"stream": false,
		"format": orderSchema,
		// Ranking is a judgement we want to be reproducible across identical
		// questions; sampling would make the same query reorder differently
		// on each ask.
		// Neither of these makes the ranking reproducible, and that is worth
		// stating plainly: with temperature 0 and a fixed seed, this model
		// still returned a different order for byte-identical requests --
		// a corpus question flipped in 2 runs out of 6 while retrieval
		// stayed stable across 5. The remaining variance is in inference
		// itself. They are set because sampling would make it worse, not
		// because they make it deterministic. Treat any single evaluation
		// run as having roughly +/-1 question of noise.
		"options": map[string]any{"temperature": 0, "seed": 42},
	})
	if err != nil {
		return nil, fmt.Errorf("rerank: encode request: %w", err)
	}

	url := strings.TrimSuffix(o.BaseURL, "/") + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("rerank: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		// An unpulled model is a 404 whose body names it, and that is the
		// likeliest misconfiguration here, so pass the server's words on.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("rerank: %s returned %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("rerank: decode response: %w", err)
	}
	var ranking struct {
		Order []int `json:"order"`
	}
	if err := json.Unmarshal([]byte(out.Message.Content), &ranking); err != nil {
		return nil, fmt.Errorf("rerank: decode ranking %q: %w", truncate(out.Message.Content, 200), err)
	}
	if len(ranking.Order) == 0 {
		return nil, fmt.Errorf("rerank: model returned an empty ranking")
	}
	return Reorder(chunks, ranking.Order), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
