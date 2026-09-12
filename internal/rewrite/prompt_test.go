package rewrite

import (
	"strings"
	"testing"

	"github.com/example/knowledge-assistant/internal/rag"
)

func TestParseQueryStripsReasoning(t *testing.T) {
	got, err := parseQuery(`We resolve the reference. {"query":"Louisiana data call deadline"}`)
	if err != nil {
		t.Fatalf("parseQuery: %v", err)
	}
	if got != "Louisiana data call deadline" {
		t.Fatalf("got %q", got)
	}
}

func TestParseQueryEmpty(t *testing.T) {
	if _, err := parseQuery(`{"query":"  "}`); err == nil {
		t.Fatal("expected error on empty query")
	}
}

func TestBuildRewritePromptTrimsHistory(t *testing.T) {
	hist := make([]rag.Turn, 10)
	for i := range hist {
		hist[i] = rag.Turn{Role: "user", Content: "x"}
	}
	p := buildRewritePrompt("latest?", hist, 6)
	if !strings.Contains(p, "Latest user message:") {
		t.Fatalf("prompt missing latest marker:\n%s", p)
	}
}
