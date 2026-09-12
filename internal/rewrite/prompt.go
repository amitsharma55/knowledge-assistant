package rewrite

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/knowledge-assistant/internal/rag"
)

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

// buildRewritePrompt renders the user turn: the recent history (trimmed to
// maxTurns; 0 means no trim here) and the latest message.
func buildRewritePrompt(question string, history []rag.Turn, maxTurns int) string {
	if maxTurns > 0 && len(history) > maxTurns {
		history = history[len(history)-maxTurns:]
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
	return u.String()
}

// parseQuery reads {"query":"..."} out of a model reply, tolerating reasoning
// text around the JSON object (Bedrock's Converse may wrap it in prose where
// Ollama's structured output does not).
func parseQuery(content string) (string, error) {
	i, j := strings.IndexByte(content, '{'), strings.LastIndexByte(content, '}')
	if i < 0 || j < i {
		return "", fmt.Errorf("rewrite: decode query %q: no JSON object found", truncate(content, 200))
	}
	var parsed struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(content[i:j+1]), &parsed); err != nil {
		return "", fmt.Errorf("rewrite: decode query %q: %w", truncate(content, 200), err)
	}
	if strings.TrimSpace(parsed.Query) == "" {
		return "", fmt.Errorf("rewrite: model returned an empty query")
	}
	return strings.TrimSpace(parsed.Query), nil
}
