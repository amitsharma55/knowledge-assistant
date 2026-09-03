package bedrock

import (
	"context"
	"strings"
	"time"

	"github.com/example/knowledge-assistant/internal/rag"
)

// MockLLM streams a canned answer that echoes retrieved chunks so devs can
// exercise the full pipeline without spending Bedrock tokens.
type MockLLM struct{}

func (MockLLM) Stream(ctx context.Context, prompt rag.Prompt, out chan<- rag.StreamEvent) error {
	answer := deriveAnswer(prompt.User)
	for _, tok := range strings.SplitAfter(answer, " ") {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- rag.StreamEvent{Type: "token", Data: tok}:
		}
		time.Sleep(15 * time.Millisecond)
	}
	return nil
}

// deriveAnswer produces a plausible mock answer by pulling the first sentence
// from each context block in the prompt.
func deriveAnswer(prompt string) string {
	ctxStart := strings.Index(prompt, "<context>")
	ctxEnd := strings.Index(prompt, "</context>")
	if ctxStart < 0 || ctxEnd < 0 {
		return "I don't have that information in the documentation I can see."
	}
	body := prompt[ctxStart+len("<context>") : ctxEnd]
	blocks := strings.Split(body, "---")
	var out strings.Builder
	out.WriteString("Based on the documentation:\n")
	for i, blk := range blocks {
		blk = strings.TrimSpace(blk)
		if blk == "" {
			continue
		}
		// take first non-header line
		for _, line := range strings.Split(blk, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "source=") {
				continue
			}
			out.WriteString("- ")
			out.WriteString(line)
			out.WriteString(" [")
			out.WriteString(itoa(i + 1))
			out.WriteString("]\n")
			break
		}
	}
	return out.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
