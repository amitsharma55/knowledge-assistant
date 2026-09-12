package rerank

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/knowledge-assistant/internal/rag"
)

const systemPrompt = `You are a document re-ranker.
You are given a question and a numbered list of text chunks retrieved from a knowledge base.
The chunks are in the order retrieval returned them, which is approximately by relevance; you can usually improve on it.
Rank every chunk by how well it answers the question, most relevant first.
Judge whether a chunk contains the specific facts the question asks for, not merely whether it covers the same topic.
Reply only with the ranked chunk ids. Include every id you were given, exactly once.`

// buildRerankPrompt renders the user turn: the question and the numbered
// chunks, in retrieval order.
func buildRerankPrompt(query string, chunks []rag.Chunk) string {
	var u strings.Builder
	fmt.Fprintf(&u, "Question:\n\n%s\n\nChunks:\n\n", query)
	for i, c := range chunks {
		fmt.Fprintf(&u, "# CHUNK ID: %d\nsource: %s — %s\n\n%s\n\n", i+1, c.PageTitle, c.SectionPath, c.Text)
	}
	fmt.Fprintf(&u, "Rank all %d chunk ids by relevance to the question, most relevant first.", len(chunks))
	return u.String()
}

// parseOrder reads {"order":[ints]} out of a model reply, tolerating any
// reasoning text around the JSON object. Ollama's structured output returns
// clean JSON; Bedrock's Converse may wrap it in prose, so extraction is done
// here rather than relying on the transport.
func parseOrder(content string) ([]int, error) {
	obj, err := extractJSONObject(content)
	if err != nil {
		return nil, fmt.Errorf("rerank: decode ranking %q: %w", truncate(content, 200), err)
	}
	var ranking struct {
		Order []int `json:"order"`
	}
	if err := json.Unmarshal([]byte(obj), &ranking); err != nil {
		return nil, fmt.Errorf("rerank: decode ranking %q: %w", truncate(content, 200), err)
	}
	if len(ranking.Order) == 0 {
		return nil, fmt.Errorf("rerank: model returned an empty ranking")
	}
	return ranking.Order, nil
}

// extractJSONObject returns the substring from the first '{' to the last '}'.
func extractJSONObject(s string) (string, error) {
	i, j := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if i < 0 || j < i {
		return "", fmt.Errorf("no JSON object found")
	}
	return s[i : j+1], nil
}
