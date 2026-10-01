package eval

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/team"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestMetrics(t *testing.T) {
	// gains already reduced to one credit per relevant item.
	t.Run("ndcg against ideal order", func(t *testing.T) {
		gains := []float64{2, 0, 1}
		relevant := []float64{2, 1}
		// DCG = 2/log2(2) + 0 + 1/log2(4) = 2 + 0.5 = 2.5
		// IDCG (ideal [2,1]) = 2/log2(2) + 1/log2(3) = 2 + 0.6309297...
		want := 2.5 / (2 + 1/math.Log2(3))
		if got := NDCGAtK(gains, relevant, 3); !approx(got, want) {
			t.Fatalf("NDCG = %v, want %v", got, want)
		}
	})
	t.Run("perfect order scores 1", func(t *testing.T) {
		if got := NDCGAtK([]float64{2, 1}, []float64{2, 1}, 3); !approx(got, 1) {
			t.Fatalf("NDCG = %v, want 1", got)
		}
	})
	t.Run("ties: equal grades order-independent", func(t *testing.T) {
		a := NDCGAtK([]float64{1, 1}, []float64{1, 1}, 2)
		b := NDCGAtK([]float64{1, 1}, []float64{1, 1}, 2)
		if !approx(a, 1) || !approx(b, 1) {
			t.Fatalf("tied grades should score 1, got %v %v", a, b)
		}
	})
	t.Run("nothing relevant scores 0 not 1", func(t *testing.T) {
		if got := NDCGAtK(nil, nil, 10); got != 0 {
			t.Fatalf("NDCG of empty = %v, want 0", got)
		}
	})
	t.Run("recall honours totalRelevant and k", func(t *testing.T) {
		g := []float64{1, 1, 0}
		if got := RecallAtK(g, 2, 3); !approx(got, 1) {
			t.Fatalf("recall@3 = %v, want 1", got)
		}
		if got := RecallAtK(g, 2, 1); !approx(got, 0.5) {
			t.Fatalf("recall@1 = %v, want 0.5", got)
		}
		if got := RecallAtK(g, 0, 3); got != 0 {
			t.Fatalf("recall with no relevant = %v, want 0", got)
		}
	})
	t.Run("precision divides by k", func(t *testing.T) {
		if got := PrecisionAtK([]float64{1, 0}, 4); !approx(got, 0.25) {
			t.Fatalf("precision@4 = %v, want 0.25 (denominator is k, not len)", got)
		}
	})
	t.Run("mrr is first relevant rank", func(t *testing.T) {
		if got := MRR([]float64{0, 0, 3}); !approx(got, 1.0/3) {
			t.Fatalf("MRR = %v, want 1/3", got)
		}
		if got := MRR([]float64{0, 0}); got != 0 {
			t.Fatalf("MRR with no hit = %v, want 0", got)
		}
	})
}

func TestScoreCreditsEachLabelOnce(t *testing.T) {
	// Two chunks of the same page under a single page-level label: recall must
	// not double-count, and only the first gets DCG credit.
	e := Entry{Relevant: []Label{{PageID: "p", Grade: 2}}}
	ranked := []rag.Chunk{{PageID: "p", SectionPath: "A"}, {PageID: "p", SectionPath: "B"}}
	gains, relevant := score(e, ranked)
	if len(relevant) != 1 {
		t.Fatalf("relevant = %v, want one label", relevant)
	}
	if gains[0] != 2 || gains[1] != 0 {
		t.Fatalf("gains = %v, want [2 0] (second chunk re-covers a credited label)", gains)
	}
}

func TestScoreSectionLabelIsSpecific(t *testing.T) {
	e := Entry{Relevant: []Label{{PageID: "p", Section: "Timeouts and errors", Grade: 1}}}
	ranked := []rag.Chunk{{PageID: "p", SectionPath: "Request model"}, {PageID: "p", SectionPath: "Timeouts and errors"}}
	gains, _ := score(e, ranked)
	if gains[0] != 0 || gains[1] != 1 {
		t.Fatalf("gains = %v, want [0 1] (only the labelled section counts)", gains)
	}
}

func TestLoadRejectsBadEntries(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "g.json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := map[string]string{
		"empty set":        `[]`,
		"unknown category": `[{"id":"a","query":"q","team":"coupa","category":"weird","relevant":[{"pageId":"p","grade":1}]}]`,
		"no labels":        `[{"id":"a","query":"q","team":"coupa","category":"normal","relevant":[]}]`,
		"zero grade":       `[{"id":"a","query":"q","team":"coupa","category":"normal","relevant":[{"pageId":"p","grade":0}]}]`,
		"duplicate id":     `[{"id":"a","query":"q","team":"coupa","category":"normal","relevant":[{"pageId":"p","grade":1}]},{"id":"a","query":"q2","team":"coupa","category":"normal","relevant":[{"pageId":"p","grade":1}]}]`,
	}
	for name, body := range cases {
		if _, err := Load(write(body)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
	good := `[{"id":"a","query":"q","team":"coupa","category":"normal","relevant":[{"pageId":"p","grade":1}]}]`
	if _, err := Load(write(good)); err != nil {
		t.Errorf("valid set rejected: %v", err)
	}
}

// stubRetriever returns a fixed ranked list regardless of query.
type stubRetriever struct{ chunks []rag.Chunk }

func (s stubRetriever) Search(_ context.Context, _ string, _ rag.Scope, k int) ([]rag.Chunk, error) {
	if k < len(s.chunks) {
		return s.chunks[:k], nil
	}
	return s.chunks, nil
}

// reverseReranker reverses the pool, standing in for a reranker that moves the
// relevant chunk from the back to the front.
type reverseReranker struct{}

func (reverseReranker) Rerank(_ context.Context, _ string, chunks []rag.Chunk) ([]rag.Chunk, error) {
	out := make([]rag.Chunk, len(chunks))
	for i, c := range chunks {
		out[len(chunks)-1-i] = c
	}
	return out, nil
}

// failingReranker stands in for a rerank outage (unreachable, timeout, 5xx).
type failingReranker struct{}

func (failingReranker) Rerank(_ context.Context, _ string, _ []rag.Chunk) ([]rag.Chunk, error) {
	return nil, context.DeadlineExceeded
}

// countMismatchReranker returns the wrong number of chunks (a malformed model
// ranking that filtered instead of reordered).
type countMismatchReranker struct{}

func (countMismatchReranker) Rerank(_ context.Context, _ string, chunks []rag.Chunk) ([]rag.Chunk, error) {
	return chunks[:len(chunks)-1], nil
}

func TestRerankOutageFallsBackToDenseOrder(t *testing.T) {
	reg := team.NewRegistry(team.DefaultInfos())
	pool := []rag.Chunk{
		{PageID: "other", SectionPath: "x", Team: "coupa"},
		{PageID: "target", SectionPath: "y", Team: "coupa"},
	}
	entries := []Entry{{
		ID: "t1", Query: "q", Team: "coupa", Category: CategoryNormal,
		Relevant: []Label{{PageID: "target", Grade: 1}},
	}}
	for name, rr := range map[string]rag.Reranker{
		"error":          failingReranker{},
		"count mismatch": countMismatchReranker{},
	} {
		raw, reranked, err := Run(context.Background(), entries, reg, stubRetriever{pool}, rr, 8, 10)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if reranked.Overall.MRR != raw.Overall.MRR || reranked.Overall.NDCG != raw.Overall.NDCG {
			t.Errorf("%s: reranked (%v) should equal dense order (%v)", name, reranked.Overall, raw.Overall)
		}
	}
}

func TestRunRawVsReranked(t *testing.T) {
	reg := team.NewRegistry(team.DefaultInfos())
	// Relevant chunk is last in retrieval order, first after reranking.
	pool := []rag.Chunk{
		{PageID: "other", SectionPath: "x", Team: "coupa"},
		{PageID: "target", SectionPath: "y", Team: "coupa"},
	}
	entries := []Entry{{
		ID: "t1", Query: "q", Team: "coupa", Category: CategoryNearTwin,
		Relevant: []Label{{PageID: "target", Grade: 1}},
	}}
	raw, reranked, err := Run(context.Background(), entries, reg, stubRetriever{pool}, reverseReranker{}, 8, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !approx(raw.Overall.MRR, 0.5) { // relevant at rank 2
		t.Fatalf("raw MRR = %v, want 0.5", raw.Overall.MRR)
	}
	if !approx(reranked.Overall.MRR, 1) { // reranker pulls it to rank 1
		t.Fatalf("reranked MRR = %v, want 1", reranked.Overall.MRR)
	}
	if _, ok := reranked.ByCat[CategoryNearTwin]; !ok {
		t.Fatalf("missing per-category bucket for %q", CategoryNearTwin)
	}
}
