package rerank

import (
	"strings"
	"testing"

	"github.com/example/knowledge-assistant/internal/rag"
)

func TestParseOrderStripsReasoning(t *testing.T) {
	// A reasoning model may emit its thinking before the JSON.
	in := `We should rank chunk 2 first. {"order":[2,1,3]} done.`
	got, err := parseOrder(in)
	if err != nil {
		t.Fatalf("parseOrder: %v", err)
	}
	if len(got) != 3 || got[0] != 2 {
		t.Fatalf("got %v, want [2 1 3]", got)
	}
}

func TestParseOrderEmpty(t *testing.T) {
	if _, err := parseOrder(`{"order":[]}`); err == nil {
		t.Fatal("expected error on empty ranking")
	}
}

func TestBuildRerankPromptNumbersChunks(t *testing.T) {
	p := buildRerankPrompt("q?", []rag.Chunk{{Text: "a"}, {Text: "b"}})
	if !strings.Contains(p, "# CHUNK ID: 1") || !strings.Contains(p, "# CHUNK ID: 2") {
		t.Fatalf("prompt missing chunk ids:\n%s", p)
	}
}
