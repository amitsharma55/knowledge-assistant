package opensearch

import (
	"context"
	"errors"
	"math"
	"sort"
	"sync"

	"github.com/example/knowledge-assistant/internal/chunker"
	"github.com/example/knowledge-assistant/internal/rag"
)

// MemoryStore is an in-process retriever for local dev / tests.
// It stores chunks with embeddings and does brute-force cosine similarity.
type MemoryStore struct {
	mu       sync.RWMutex
	items    []indexed
	Embedder rag.Embedder
}

type indexed struct {
	chunk rag.Chunk
	vec   []float32
}

func NewMemoryStore(e rag.Embedder) *MemoryStore { return &MemoryStore{Embedder: e} }

func (m *MemoryStore) Upsert(ctx context.Context, c rag.Chunk) error {
	// Same embedding input as the indexed path (internal/ingest), so an
	// uploaded document retrieves the way the same content would if it had
	// been ingested.
	v, err := m.Embedder.Embed(ctx, chunker.EmbedText(c.PageTitle, c.SectionPath, c.Text))
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, it := range m.items {
		if it.chunk.ID == c.ID {
			m.items[i] = indexed{chunk: c, vec: v}
			return nil
		}
	}
	m.items = append(m.items, indexed{chunk: c, vec: v})
	return nil
}

func (m *MemoryStore) Search(ctx context.Context, query string, scope rag.Scope, k int) ([]rag.Chunk, error) {
	if scope.IsZero() {
		return nil, errors.New("opensearch: search called with an unscoped request")
	}
	qv, err := m.Embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	type scored struct {
		c rag.Chunk
		s float64
	}
	results := make([]scored, 0, len(m.items))
	want := scope.Team().Slug()
	for _, it := range m.items {
		if it.chunk.Team != want {
			continue
		}
		if !aclOK(it.chunk.ACLGroups, scope.Groups()) {
			continue
		}
		results = append(results, scored{it.chunk, similarity(qv, it.vec)})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].s > results[j].s })
	if len(results) > k {
		results = results[:k]
	}
	out := make([]rag.Chunk, len(results))
	for i, r := range results {
		out[i] = r.c
		out[i].Score = r.s
	}
	return out, nil
}

// Count reports how many chunks in the scope's team score at or above floor
// against the query, without returning any of them. Search itself applies no
// relevance threshold (its ranking must not change, since it feeds the
// prompt), so Count applies the floor here, over Search's full result set.
func (m *MemoryStore) Count(ctx context.Context, query string, scope rag.Scope, floor float64) (int, error) {
	hits, err := m.Search(ctx, query, scope, 1000)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, h := range hits {
		if h.Score >= floor {
			n++
		}
	}
	return n, nil
}

func aclOK(chunkGroups, userGroups []string) bool {
	if len(chunkGroups) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(userGroups))
	for _, g := range userGroups {
		set[g] = struct{}{}
	}
	for _, g := range chunkGroups {
		if _, ok := set[g]; ok {
			return true
		}
	}
	return false
}

// similarity scores a pair on the same scale OpenSearch reports, so that
// KA_RELEVANCE_FLOOR means one thing regardless of which retriever is wired.
// Lucene's cosinesimil space maps cosine [-1,1] onto [0,1] as (1+cos)/2;
// raw cosine here would make a floor tuned against OpenSearch far too
// permissive in fixtures mode, and one tuned in fixtures mode unreachable
// against OpenSearch. The mapping is monotonic, so ranking is unchanged.
func similarity(a, b []float32) float64 {
	return (1 + cosine(a, b)) / 2
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
