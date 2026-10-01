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

func TestParseOrderBareList(t *testing.T) {
	// gpt-oss on Bedrock replies with a bare list and no JSON object. This is
	// the exact string that used to fall back to dense order on every call.
	for _, in := range []string{"2, 1, 3", "[2,1,3]", "2 1 3", "2,1,3\n"} {
		got, err := parseOrder(in)
		if err != nil {
			t.Fatalf("parseOrder(%q): %v", in, err)
		}
		if len(got) != 3 || got[0] != 2 || got[1] != 1 || got[2] != 3 {
			t.Fatalf("parseOrder(%q) = %v, want [2 1 3]", in, got)
		}
	}
}

func TestParseOrderRefusesProse(t *testing.T) {
	// A reply mixing prose with numbers must error (and so fall back loudly),
	// not have ids mined out of the sentence.
	for _, in := range []string{"The order is 2, 1, 3", "I cannot rank these", ""} {
		if _, err := parseOrder(in); err == nil {
			t.Fatalf("parseOrder(%q): expected error, got nil", in)
		}
	}
}

func TestBuildRerankPromptNumbersChunks(t *testing.T) {
	p := buildRerankPrompt("q?", []rag.Chunk{{Text: "a"}, {Text: "b"}})
	if !strings.Contains(p, "# CHUNK ID: 1") || !strings.Contains(p, "# CHUNK ID: 2") {
		t.Fatalf("prompt missing chunk ids:\n%s", p)
	}
}
