// Package eval measures retrieval quality over a golden set: recall@K and
// precision@K on the raw retrieval pool (the recall ceiling no reranker can
// exceed), MRR, and graded NDCG@K after reranking.
//
// It exists so a retrieval change is judged on numbers, not vibes -- and,
// crucially, PER CATEGORY. A near-twin regression and a literal-token lift have
// to be visible as separate numbers; a blended average is what made the old
// 36-question fact-coverage check useless for the hybrid-retrieval decision
// (see the note on opensearch.Client.Search).
package eval

import (
	"math"
	"sort"
)

// The metric functions below take gains already reduced to one credit per
// relevant item (see score): gains[i] is the graded relevance earned at rank i
// (0 if the chunk there is irrelevant or re-covers an already-credited item),
// and relevant is the grade of every item that should have matched. Keeping the
// math pure of chunk/label matching is what makes it unit-testable.

// dcg is discounted cumulative gain over the first k positions, with the
// standard log2(rank+1) discount (rank is 1-based, so position i contributes
// gains[i]/log2(i+2)).
func dcg(gains []float64, k int) float64 {
	sum := 0.0
	for i := 0; i < k && i < len(gains); i++ {
		sum += gains[i] / math.Log2(float64(i+2))
	}
	return sum
}

// NDCGAtK normalizes DCG@k of the ranked gains against the ideal ordering of
// all relevant grades. 1.0 means the top k hold the most relevant items in the
// best possible order. It is 0 when nothing is relevant (nothing to rank), so
// an empty gold entry scores 0 rather than a misleading 1.
func NDCGAtK(gains, relevant []float64, k int) float64 {
	ideal := append([]float64(nil), relevant...)
	sort.Sort(sort.Reverse(sort.Float64Slice(ideal)))
	norm := dcg(ideal, k)
	if norm == 0 {
		return 0
	}
	return dcg(gains, k) / norm
}

// RecallAtK is the fraction of all relevant items whose covering chunk lands in
// the top k. gains must carry one credit per relevant item (score guarantees
// it), so a page split across several chunks cannot count more than once.
func RecallAtK(gains []float64, totalRelevant, k int) float64 {
	if totalRelevant <= 0 {
		return 0
	}
	return float64(hitsInTopK(gains, k)) / float64(totalRelevant)
}

// PrecisionAtK is the fraction of the top k positions that are relevant. The
// denominator is k, not min(k, len): a query that returns fewer than k chunks
// is less precise, not artificially perfect.
func PrecisionAtK(gains []float64, k int) float64 {
	if k <= 0 {
		return 0
	}
	return float64(hitsInTopK(gains, k)) / float64(k)
}

// MRR is the reciprocal of the 1-based rank of the first relevant item, or 0 if
// none is relevant.
func MRR(gains []float64) float64 {
	for i, g := range gains {
		if g > 0 {
			return 1 / float64(i+1)
		}
	}
	return 0
}

func hitsInTopK(gains []float64, k int) int {
	n := 0
	for i := 0; i < k && i < len(gains); i++ {
		if gains[i] > 0 {
			n++
		}
	}
	return n
}
