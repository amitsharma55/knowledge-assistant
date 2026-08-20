package rag

import (
	"fmt"
	"strings"
)

const systemPrompt = `You are a knowledge assistant for internal integrations.
Answer ONLY from the provided <context> blocks. If the answer is not present, reply:
"I don't have that information in the documentation I can see."

Rules:
- Cite every factual claim with [n] matching the context block number.
- Prefer exact field names, job names, and schedule strings as written.
- Be concise. Use bullet lists when enumerating fields or jobs.
- Never invent field names, endpoints, or schedules.`

// BuildPrompt returns the full prompt string sent to Claude.
// Chunks are ordered by rerank score, most relevant first.
func BuildPrompt(question string, chunks []Chunk) string {
	var b strings.Builder
	b.WriteString(systemPrompt)
	b.WriteString("\n\n<context>\n")
	for i, c := range chunks {
		fmt.Fprintf(&b, "[%d] source=%q section=%q url=%s\n%s\n---\n",
			i+1, c.PageTitle, c.SectionPath, c.URL, c.Text)
	}
	b.WriteString("</context>\n\n")
	fmt.Fprintf(&b, "Question: %s\n\nAnswer:", question)
	return b.String()
}
