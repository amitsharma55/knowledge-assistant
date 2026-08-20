package opensearch

import (
	"context"
	"math"
	"sort"
	"sync"

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
	v, err := m.Embedder.Embed(ctx, c.Text)
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

func (m *MemoryStore) Search(ctx context.Context, query string, groups []string, k int) ([]rag.Chunk, error) {
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
	for _, it := range m.items {
		if !aclOK(it.chunk.ACLGroups, groups) {
			continue
		}
		results = append(results, scored{it.chunk, cosine(qv, it.vec)})
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
