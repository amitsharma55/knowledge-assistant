// Package rewrite makes a conversational follow-up searchable.
//
// Retrieval embeds the literal question. That is fine for a first turn and
// useless for a second: asked "what data fields are involved?" and then
// "I am asking about AVR", the search runs on the six words of the reply,
// which match the AVR service overview far better than the field mapping the
// user is actually after. The answering model has the history and can resolve
// the reference itself; the vector index cannot, so the query is resolved for
// it before the search.
package rewrite

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

// Ollama rewrites with a chat model served by a local Ollama instance.
type Ollama struct {
	BaseURL string
	Model   string
	HTTP    *http.Client
	// MaxTurns caps how much history is shown. Zero means 6. Only the recent
	// turns disambiguate a follow-up, and an unbounded transcript would grow
	// the prompt without bound.
	MaxTurns int
}

const systemPrompt = `You are in a conversation with a user, answering questions about the integrations documented in a knowledge base. You are about to look up information there to answer them.

Write the query you will search with.

It must do two jobs:
- Resolve the question against the conversation. A latest message may be a fragment or an answer to something you just asked; replace pronouns and references with what they refer to, and carry the subject forward.
- Leave it alone otherwise. Do not re-word a question that already stands on its own; return it unchanged.

Rules:
- Reply with the search query only. No preamble, no explanation, no quotes.
- Keep every proper noun the user used, and add the one the conversation supplies.
- Never narrow past what was asked. A question about several things stays about all of them.

Rewording a question that needs no rewording is not free. Compressing "What is the submission deadline for the Louisiana data call?" into bare terms costs the phrasing that separates it from the near-identical Texas document, and the search then straddles both.`

const orderSchemaKey = "query"

var querySchema = map[string]any{
	"type":       "object",
	"properties": map[string]any{orderSchemaKey: map[string]any{"type": "string"}},
	"required":   []string{orderSchemaKey},
}

func (o Ollama) client() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return http.DefaultClient
}

func (o Ollama) maxTurns() int {
	if o.MaxTurns > 0 {
		return o.MaxTurns
	}
	return 6
}

// Rewrite returns the search query for question given history.
// Whether a first turn is worth rewriting is the orchestrator's call, not
// this client's: it depends on whether the original question is being
// searched alongside. So no history is a normal input here, not a shortcut.
func (o Ollama) Rewrite(ctx context.Context, question string, history []rag.Turn) (string, error) {
	if n := o.maxTurns(); len(history) > n {
		history = history[len(history)-n:]
	}

	var u strings.Builder
	if len(history) == 0 {
		u.WriteString("There is no conversation yet; this is the user's first message.\n\n")
	} else {
		u.WriteString("Conversation so far:\n\n")
		for _, t := range history {
			fmt.Fprintf(&u, "%s: %s\n\n", t.Role, truncate(t.Content, 1500))
		}
	}
	fmt.Fprintf(&u, "Latest user message:\n\n%s\n\nWrite the knowledge base search query.", question)

	payload, err := json.Marshal(map[string]any{
		"model": o.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": u.String()},
		},
		"stream": false,
		"format": querySchema,
		// Neither of these makes the query reproducible, and that is worth
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
		return "", fmt.Errorf("rewrite: encode request: %w", err)
	}

	url := strings.TrimSuffix(o.BaseURL, "/") + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("rewrite: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("rewrite: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("rewrite: %s returned %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("rewrite: decode response: %w", err)
	}
	var parsed struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(out.Message.Content), &parsed); err != nil {
		return "", fmt.Errorf("rewrite: decode query %q: %w", truncate(out.Message.Content, 200), err)
	}
	if strings.TrimSpace(parsed.Query) == "" {
		return "", fmt.Errorf("rewrite: model returned an empty query")
	}
	return strings.TrimSpace(parsed.Query), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
