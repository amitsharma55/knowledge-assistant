package eval

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/team"
)

// Metrics are the averaged scores for a set of queries at a fixed K. Precision
// is reported at ranks 1 and 3, not at K: with ~1 relevant chunk per query,
// precision@10 cannot exceed ~0.1 and carries no signal, whereas "is the top
// hit relevant" (P@1) and "is it in the top 3" (P@3) do.
type Metrics struct {
	Queries int
	Recall  float64
	P1      float64
	P3      float64
	MRR     float64
	NDCG    float64
}

// Latency summarises the per-query rerank round-trip. It is the added cost of
// turning the reranker on -- the number the A/B weighs the quality gain against.
// Percentiles use nearest-rank; over a handful of queries p95/p99 are not yet
// meaningful (they collapse onto the max), which is one more reason the golden
// set has to grow before this decides anything.
type Latency struct {
	N                  int
	P50, P95, P99, Max time.Duration
}

// Result is one pipeline stage's metrics, overall and broken out per category.
// Latency is populated only for the reranked stage (it is the rerank call's
// time); it is the zero value when the reranker is nil.
type Result struct {
	Overall Metrics
	ByCat   map[string]Metrics
	Latency Latency
	// Fallbacks counts queries where the reranker returned an error or the wrong
	// chunk count and the pool was kept in dense order. Nonzero means the
	// reranker did little or nothing -- the signal that hid the Bedrock
	// no-op, so the runner surfaces it loudly.
	Fallbacks int
}

// Run executes the retrieval pipeline over the golden set and returns metrics
// for two stages: raw (the retrieval pool straight from the retriever -- the
// recall ceiling) and reranked (after rr reorders it; identical to raw when rr
// is nil). Pass rr=nil for the dense-only baseline and a real reranker to see
// whether reranking alone fixes a category.
//
// It mirrors the single-turn production path -- retriever.Search at topK, then
// the reranker with the orchestrator's exact defensive fallback -- and nothing
// else. Query rewrite and dual retrieval are deliberately not exercised: golden
// queries are already self-contained, and those stages are a separate question
// from the recall ceiling this harness measures.
func Run(ctx context.Context, entries []Entry, reg *team.Registry, r rag.Retriever, rr rag.Reranker, topK, k int) (raw, reranked Result, err error) {
	var rawAcc, rrAcc accumulator
	var rerankTimes []time.Duration
	var fallbacks int
	for _, e := range entries {
		t, perr := reg.Parse(e.Team)
		if perr != nil {
			return Result{}, Result{}, perr
		}
		// groups nil -> only unrestricted chunks, which is all fixtures carry.
		scope := rag.NewScope(t, nil, nil)
		pool, serr := r.Search(ctx, e.Query, scope, topK)
		if serr != nil {
			return Result{}, Result{}, serr
		}
		rec, p1, p3, mr, nd := metricsFor(e, pool, k)
		rawAcc.add(e, rec, p1, p3, mr, nd)

		start := time.Now()
		ranked, fellBack := rerankOrKeep(ctx, rr, e.Query, pool)
		rerankTimes = append(rerankTimes, time.Since(start))
		if fellBack {
			fallbacks++
		}
		rec, p1, p3, mr, nd = metricsFor(e, ranked, k)
		rrAcc.add(e, rec, p1, p3, mr, nd)
	}
	reranked = rrAcc.result()
	reranked.Latency = percentiles(rerankTimes)
	reranked.Fallbacks = fallbacks
	return rawAcc.result(), reranked, nil
}

// percentiles summarises durations with the nearest-rank method.
func percentiles(ds []time.Duration) Latency {
	if len(ds) == 0 {
		return Latency{}
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	at := func(q float64) time.Duration {
		i := int(math.Ceil(q*float64(len(s)))) - 1
		if i < 0 {
			i = 0
		}
		if i >= len(s) {
			i = len(s) - 1
		}
		return s[i]
	}
	return Latency{N: len(s), P50: at(0.50), P95: at(0.95), P99: at(0.99), Max: s[len(s)-1]}
}

// rerankOrKeep reorders the pool, falling back to retrieval order on any failure
// or count mismatch -- the same contract as Orchestrator.reranked, so the
// harness measures what production would actually serve. fellBack reports that a
// fallback happened (reranker present but unusable), so the runner can surface a
// no-op reranker instead of reporting a silent zero delta.
func rerankOrKeep(ctx context.Context, rr rag.Reranker, query string, chunks []rag.Chunk) (ranked []rag.Chunk, fellBack bool) {
	if rr == nil || len(chunks) < 2 {
		return chunks, false
	}
	out, err := rr.Rerank(ctx, query, chunks)
	if err != nil || len(out) != len(chunks) {
		return chunks, true
	}
	return out, false
}

// metricsFor scores one entry's ranked chunk list. Recall and NDCG are at K;
// precision is at 1 and 3 (see Metrics).
func metricsFor(e Entry, ranked []rag.Chunk, k int) (recall, p1, p3, mrr, ndcg float64) {
	gains, relevant := score(e, ranked)
	return RecallAtK(gains, len(relevant), k),
		PrecisionAtK(gains, 1),
		PrecisionAtK(gains, 3),
		MRR(gains),
		NDCGAtK(gains, relevant, k)
}

// score reduces a ranked chunk list to per-rank gains for one entry, crediting
// each relevant label only at the earliest rank that covers it. A later chunk
// re-covering an already-credited label contributes 0, so a page split into
// several chunks cannot inflate recall or DCG.
func score(e Entry, ranked []rag.Chunk) (gains, relevant []float64) {
	relevant = make([]float64, len(e.Relevant))
	for i, l := range e.Relevant {
		relevant[i] = l.Grade
	}
	credited := make([]bool, len(e.Relevant))
	gains = make([]float64, len(ranked))
	for i, c := range ranked {
		best, bestIdx := 0.0, -1
		for j, l := range e.Relevant {
			if credited[j] || !l.matches(c) {
				continue
			}
			if l.Grade > best {
				best, bestIdx = l.Grade, j
			}
		}
		if bestIdx >= 0 {
			credited[bestIdx] = true
			gains[i] = best
		}
	}
	return gains, relevant
}

// accumulator sums per-query metrics overall and per category, then averages.
type accumulator struct {
	overall sums
	byCat   map[string]*sums
}

type sums struct {
	n                         int
	recall, p1, p3, mrr, ndcg float64
}

func (s *sums) add(recall, p1, p3, mrr, ndcg float64) {
	s.n++
	s.recall += recall
	s.p1 += p1
	s.p3 += p3
	s.mrr += mrr
	s.ndcg += ndcg
}

func (s sums) mean() Metrics {
	if s.n == 0 {
		return Metrics{}
	}
	d := float64(s.n)
	return Metrics{Queries: s.n, Recall: s.recall / d, P1: s.p1 / d, P3: s.p3 / d, MRR: s.mrr / d, NDCG: s.ndcg / d}
}

func (a *accumulator) add(e Entry, recall, p1, p3, mrr, ndcg float64) {
	if a.byCat == nil {
		a.byCat = map[string]*sums{}
	}
	a.overall.add(recall, p1, p3, mrr, ndcg)
	c := a.byCat[e.Category]
	if c == nil {
		c = &sums{}
		a.byCat[e.Category] = c
	}
	c.add(recall, p1, p3, mrr, ndcg)
}

func (a *accumulator) result() Result {
	byCat := make(map[string]Metrics, len(a.byCat))
	for cat, s := range a.byCat {
		byCat[cat] = s.mean()
	}
	return Result{Overall: a.overall.mean(), ByCat: byCat}
}
