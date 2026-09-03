package rerank

import (
	"testing"

	"github.com/example/knowledge-assistant/internal/rag"
)

func chunks(ids ...string) []rag.Chunk {
	out := make([]rag.Chunk, len(ids))
	for i, id := range ids {
		out[i] = rag.Chunk{ID: id}
	}
	return out
}

func ids(cs []rag.Chunk) string {
	s := ""
	for _, c := range cs {
		s += c.ID
	}
	return s
}

func TestReorderAppliesRanking(t *testing.T) {
	got := Reorder(chunks("a", "b", "c"), []int{3, 1, 2})
	if ids(got) != "cab" {
		t.Errorf("got %q, want %q", ids(got), "cab")
	}
}

func TestReorderKeepsChunksTheModelOmitted(t *testing.T) {
	// gpt-oss returned 19 ids for 20 chunks on the first real call. A
	// dropped id must not drop the chunk.
	got := Reorder(chunks("a", "b", "c", "d"), []int{4, 2})
	if ids(got) != "dbac" {
		t.Errorf("got %q, want ranked ids first then the rest in retrieval order (dbac)", ids(got))
	}
	if len(got) != 4 {
		t.Errorf("chunk count changed: %d, want 4", len(got))
	}
}

func TestReorderIgnoresBadIDs(t *testing.T) {
	// Out of range, zero, negative and repeated ids are all things models
	// emit; none may duplicate or drop a chunk.
	got := Reorder(chunks("a", "b", "c"), []int{99, 2, 2, 0, -1, 3})
	if ids(got) != "bca" {
		t.Errorf("got %q, want %q", ids(got), "bca")
	}
}

func TestReorderEmptyRankingKeepsOrder(t *testing.T) {
	got := Reorder(chunks("a", "b"), nil)
	if ids(got) != "ab" {
		t.Errorf("got %q, want retrieval order preserved", ids(got))
	}
}
