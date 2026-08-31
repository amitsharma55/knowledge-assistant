package opensearch

import (
	"context"
	"testing"

	"strconv"

	"github.com/example/knowledge-assistant/internal/embed"
	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/team"
)

func scopeFor(t *testing.T, slug string, allowed ...string) rag.Scope {
	t.Helper()
	r := team.NewRegistry(team.DefaultInfos())
	active, err := r.Parse(slug)
	if err != nil {
		t.Fatalf("parse %q: %v", slug, err)
	}
	var as []team.Team
	for _, a := range allowed {
		p, err := r.Parse(a)
		if err != nil {
			t.Fatalf("parse %q: %v", a, err)
		}
		as = append(as, p)
	}
	return rag.NewScope(active, as, nil)
}

// seed inserts chunks whose text makes the HR chunks the nearest neighbours
// of the query, so a search that forgets to filter will surface them.
func seed(t *testing.T, m *MemoryStore) {
	t.Helper()
	ctx := context.Background()
	docs := []rag.Chunk{
		{ID: "hr-1", Team: "hr", PageTitle: "Escalation policy", Text: "escalation policy escalation policy"},
		{ID: "hr-2", Team: "hr", PageTitle: "Escalation matrix", Text: "escalation policy escalation matrix"},
		{ID: "coupa-1", Team: "coupa", PageTitle: "Invoice sync", Text: "invoice synchronisation with the supplier ledger"},
	}
	for _, d := range docs {
		if err := m.Upsert(ctx, d); err != nil {
			t.Fatalf("upsert %s: %v", d.ID, err)
		}
	}
}

func TestSearchNeverReturnsAnotherTeamsChunks(t *testing.T) {
	m := NewMemoryStore(embed.Mock{Dim: 1024})
	seed(t, m)

	got, err := m.Search(context.Background(), "escalation policy", scopeFor(t, "coupa", "coupa"), 8)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, c := range got {
		if c.Team != "coupa" {
			t.Fatalf("Search under coupa returned chunk %q from team %q", c.ID, c.Team)
		}
	}
}

// Spec test 3: the regression test for the post_filter starvation bug. The
// scoped team's corpus is larger than k, and another team's is larger still.
func TestSearchReturnsFullKWithinTheTeamPartition(t *testing.T) {
	m := NewMemoryStore(embed.Mock{Dim: 1024})
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		id := "hr-" + strconv.Itoa(i)
		if err := m.Upsert(ctx, rag.Chunk{ID: id, Team: "hr", Text: "escalation policy " + id}); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}
	for i := 0; i < 6; i++ {
		id := "coupa-" + strconv.Itoa(i)
		if err := m.Upsert(ctx, rag.Chunk{ID: id, Team: "coupa", Text: "escalation policy " + id}); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}

	got, err := m.Search(ctx, "escalation policy", scopeFor(t, "coupa", "coupa"), 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("Search returned %d chunks, want 5; the team filter must be applied before k, not after", len(got))
	}
}

// TestCountOnlyCountsChunksAtOrAboveFloor pins the fix for a bug where Count
// returned len(Search(..., 1000)) unconditionally — i.e. the size of the
// team's visible corpus, independent of the query, since Search itself
// applies no relevance threshold. Count must apply the floor itself.
func TestCountOnlyCountsChunksAtOrAboveFloor(t *testing.T) {
	m := NewMemoryStore(embed.Mock{Dim: 1024})
	ctx := context.Background()
	docs := []rag.Chunk{
		{ID: "hr-1", Team: "hr", Text: "parental leave policy details"},
		{ID: "hr-2", Team: "hr", Text: "parental leave policy details"},
		{ID: "hr-3", Team: "hr", Text: "expense reimbursement procedure"},
	}
	for _, d := range docs {
		if err := m.Upsert(ctx, d); err != nil {
			t.Fatalf("upsert %s: %v", d.ID, err)
		}
	}
	scope := scopeFor(t, "hr", "hr")

	// A query matching real content: an exact text match embeds identically
	// under the mock embedder, scoring cosine 1.0, well above the floor. The
	// third chunk's independently-hashed embedding is uncorrelated with the
	// query and should not count.
	n, err := m.Count(ctx, "parental leave policy details", scope, 0.5)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 2 {
		t.Fatalf("Count = %d, want 2 (only the exact-text matches), not the whole team corpus", n)
	}

	// A query with nothing relevant to any chunk in the team: under the mock
	// embedder distinct text hashes to an effectively uncorrelated vector, so
	// every chunk should score well below the floor.
	n, err = m.Count(ctx, "completely unrelated gibberish about deep sea navigation", scope, 0.5)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Fatalf("Count = %d, want 0 for a query with nothing relevant; Count must not just report corpus size", n)
	}
}

func TestSearchRefusesZeroScope(t *testing.T) {
	m := NewMemoryStore(embed.Mock{Dim: 1024})
	seed(t, m)

	if _, err := m.Search(context.Background(), "escalation policy", rag.Scope{}, 8); err == nil {
		t.Fatal("Search with a zero Scope returned nil error, want refusal")
	}
}
