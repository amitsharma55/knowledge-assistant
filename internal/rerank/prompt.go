package rerank

import (
	"encoding/json"
	"fmt"
	"strconv"
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

// parseOrder reads the ranking out of a model reply.
//
// Preferred form is {"order":[ints]}, tolerating reasoning text around it:
// Ollama's structured output guarantees that shape. Bedrock's Converse has no
// such enforcement, and gpt-oss there replies with a bare list -- "2, 1, 3" --
// which has no JSON object at all. That parsed as an error and fell back to
// dense order on every call, silently costing all reranking, so a bare integer
// list is now accepted too.
//
// The bare-list path is deliberately strict: it accepts a reply that is nothing
// but integers and separators, and refuses one that also contains prose rather
// than guess ids out of a sentence. A refusal surfaces as a loud fallback (see
// the reranker callers); if that starts happening, enforce structured output on
// Bedrock instead of loosening this further.
func parseOrder(content string) ([]int, error) {
	if obj, err := extractJSONObject(content); err == nil {
		var ranking struct {
			Order []int `json:"order"`
		}
		if err := json.Unmarshal([]byte(obj), &ranking); err == nil && len(ranking.Order) > 0 {
			return ranking.Order, nil
		}
	}
	if ids := looseInts(content); len(ids) > 0 {
		return ids, nil
	}
	return nil, fmt.Errorf("rerank: no ranking in reply %q", truncate(content, 200))
}

// looseInts parses a reply that is nothing but a list of integers, e.g.
// "2, 1, 3" or "[2,1,3]". It returns nil the moment any token is not an integer,
// so prose around the numbers is refused rather than mined for ids.
func looseInts(s string) []int {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ',', ' ', '\t', '\n', '\r', '[', ']':
			return true
		default:
			return false
		}
	})
	if len(fields) == 0 {
		return nil
	}
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// extractJSONObject returns the substring from the first '{' to the last '}'.
func extractJSONObject(s string) (string, error) {
	i, j := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if i < 0 || j < i {
		return "", fmt.Errorf("no JSON object found")
	}
	return s[i : j+1], nil
}
