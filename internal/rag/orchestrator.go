package rag

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

type Orchestrator struct {
	Retriever      Retriever
	LLM            LLM
	TopK           int
	RerankN        int
	Counter        Counter
	RelevanceFloor float64
	// Reranker reorders the retrieval pool before RerankN selects from it.
	// Optional; nil leaves chunks in retrieval order, which is what
	// RerankN cut for as long as no reranker existed.
	Reranker Reranker
	// Rewriter makes a follow-up self-contained before it is used as a
	// search query. Optional; nil retrieves on the user's literal words.
	Rewriter Rewriter
	// DualRetrieval searches on both the question as asked and its rewrite,
	// then merges. Off, the rewrite replaces the question, so a bad rewrite
	// loses good chunks with nothing to recover them; on, the original
	// always contributes and the rewrite can only add. It costs a second
	// search and a larger pool for the reranker to sort.
	DualRetrieval bool
	// MaxContext caps the prompt after sibling backfill. RerankN alone
	// decides how many chunks are selected on relevance; backfill may push
	// past it to keep a document whole, but never past MaxContext. Zero --
	// the normal setting -- means the retrieval pool, TopK: backfill may
	// use anything already retrieved and nothing more.
	//
	// It is raised to RerankN when set lower. A smaller value cannot cap
	// anything (selection has already produced RerankN chunks) and only
	// makes backfill exit as a silent no-op.
	MaxContext int
	// Log receives operational signals that don't warrant aborting the
	// answer, such as a failed session retrieval. Optional; nil disables
	// logging (falls back to slog.Default()).
	Log *slog.Logger
}

func (o *Orchestrator) logger() *slog.Logger {
	if o.Log != nil {
		return o.Log
	}
	return slog.Default()
}

// Answer runs the full retrieve → prompt → stream pipeline. If `session` is
// non-nil, its chunks are included first (user's uploaded doc = explicit
// intent), then filled with the primary retriever's chunks up to RerankN.
func (o *Orchestrator) Answer(ctx context.Context, question string, history []Turn, scope Scope, session Retriever, out chan<- StreamEvent) error {
	// A greeting or pleasantry ("hi", "how are you?", "thanks") gets a warm
	// reply and no document search: retrieval scores can't tell small talk
	// from a real question on this embedder, so we gate on the message itself,
	// before spending an embed + vector search on it. The check is fail-safe
	// -- anything that isn't clearly small talk falls through to retrieval --
	// so a real question is never mistaken for a greeting and refused.
	if isGreeting(question) {
		out <- StreamEvent{Type: "retrieval", Data: []RetrievedChunk{}}
		out <- StreamEvent{Type: "citation", Data: []Chunk{}}
		if err := o.LLM.Stream(ctx, BuildConversationalPrompt(question, history), out); err != nil {
			return fmt.Errorf("llm: %w", err)
		}
		out <- StreamEvent{Type: "done"}
		return nil
	}

	// The question the user asked is what the model answers and what the UI
	// shows. These queries are only ever used to retrieve.
	queries, rankQuery := o.queries(ctx, question, history)
	var chosen []Chunk
	var all []Chunk
	seen := map[string]struct{}{}
	seenAll := map[string]struct{}{}

	if session != nil {
		sc, err := searchAll(ctx, session, queries, scope, o.RerankN)
		if err != nil {
			// A failed session lookup (e.g. embed error) silently drops the
			// user's just-uploaded document from grounding. That's a real
			// degradation, not a fatal one — the primary retriever can still
			// answer from the KB — so we log it rather than abort the
			// request the way a primary retriever error does.
			o.logger().Warn("session retriever failed; continuing without uploaded-doc grounding", "err", err)
		} else {
			for _, c := range sc {
				if _, dup := seenAll[c.ID]; !dup {
					seenAll[c.ID] = struct{}{}
					all = append(all, c)
				}
				if _, dup := seen[c.ID]; dup {
					continue
				}
				if len(chosen) < o.RerankN {
					seen[c.ID] = struct{}{}
					chosen = append(chosen, c)
				}
			}
		}
	}

	// Always query the primary retriever, even if the session retriever
	// already filled `chosen`, so `all` (and thus the retrieval event's
	// context panel) reflects every chunk retrieved — including KB chunks
	// that went unused — per spec. Selection behaviour is unchanged: session
	// chunks are still considered first and `chosen` is capped at RerankN
	// before any KB chunk can be added to it.
	kb, err := searchAll(ctx, o.Retriever, queries, scope, o.TopK)
	if err != nil {
		return fmt.Errorf("retrieve: %w", err)
	}
	kb = o.reranked(ctx, rankQuery, kb)
	for _, c := range kb {
		if _, dup := seenAll[c.ID]; !dup {
			seenAll[c.ID] = struct{}{}
			all = append(all, c)
		}
		if _, dup := seen[c.ID]; dup {
			continue
		}
		if len(chosen) < o.RerankN {
			seen[c.ID] = struct{}{}
			chosen = append(chosen, c)
		}
	}

	// Nothing relevant here: tell the caller whether another of their teams
	// can answer, without retrieving anything from it.
	if o.Counter != nil && belowFloor(chosen, o.RelevanceFloor) {
		var suggestions []TeamSuggestion
		for _, other := range scope.Others() {
			probe := NewScope(other, nil, scope.Groups())
			n, err := o.Counter.Count(ctx, rankQuery, probe, o.RelevanceFloor)
			if err != nil || n == 0 {
				continue
			}
			suggestions = append(suggestions, TeamSuggestion{Team: other.Slug(), Matches: n})
		}
		if len(suggestions) > 0 {
			out <- StreamEvent{Type: "suggestion", Data: suggestions}
		}
	}

	selected := len(chosen)
	chosen = backfillSiblings(chosen, all, o.maxContext())

	out <- StreamEvent{Type: "retrieval", Data: mark(all, chosen, selected)}
	out <- StreamEvent{Type: "citation", Data: chosen}
	prompt := BuildPrompt(question, history, chosen)
	if err := o.LLM.Stream(ctx, prompt, out); err != nil {
		return fmt.Errorf("llm: %w", err)
	}
	out <- StreamEvent{Type: "done"}
	return nil
}

// queries returns the strings to retrieve with, and the one to rank against.
//
// Without dual retrieval the rewrite replaces the question, and only on a
// turn that has history: a first turn already stands alone, so rewriting it
// could only distort it while costing a model call on every query.
//
// With dual retrieval both are searched and merged, which changes the
// calculus. A rewrite can then only add chunks, never lose them, so it is
// worth running on a first turn too -- a vague opening question is exactly
// where a sharper query helps, and the original is still searched alongside.
//
// The rank query is the rewrite when there is one. Ranking is a judgement
// about relevance, and "I am asking about AVR" is nothing to judge against;
// its resolved form is. (answer.py reranks against the original question --
// this is a deliberate divergence.)
//
// As with the reranker, any failure falls back to the user's words rather
// than failing the request.
func (o *Orchestrator) queries(ctx context.Context, question string, history []Turn) (search []string, rank string) {
	if o.Rewriter == nil || (!o.DualRetrieval && len(history) == 0) {
		return []string{question}, question
	}
	q, err := o.Rewriter.Rewrite(ctx, question, history)
	if err != nil {
		o.logger().Warn("query rewrite failed; retrieving on the question as asked", "err", err)
		return []string{question}, question
	}
	if q = strings.TrimSpace(q); q == "" || strings.EqualFold(q, strings.TrimSpace(question)) {
		return []string{question}, question
	}
	o.logger().Info("rewrote query for retrieval", "asked", question, "searched", q, "dual", o.DualRetrieval)
	if o.DualRetrieval {
		return []string{question, q}, q
	}
	return []string{q}, q
}

// searchAll runs every query against one retriever and merges the results,
// keeping the first occurrence of each chunk. Earlier queries therefore win
// ties, which puts the user's own wording ahead of a machine rewrite of it.
//
// A merged pool is deliberately larger and noisier than either query's. That
// is only safe because the reranker sorts it afterwards -- widening retrieval
// without one is what dropped the corpus check to 27/36.
func searchAll(ctx context.Context, r Retriever, queries []string, scope Scope, k int) ([]Chunk, error) {
	var out []Chunk
	seen := map[string]struct{}{}
	for _, q := range queries {
		hits, err := r.Search(ctx, q, scope, k)
		if err != nil {
			return nil, err
		}
		for _, c := range hits {
			if _, dup := seen[c.ID]; dup {
				continue
			}
			seen[c.ID] = struct{}{}
			out = append(out, c)
		}
	}
	return out, nil
}

// reranked reorders the KB pool, falling back to retrieval order on any
// failure.
//
// A reranker is an improvement to ordering, not a dependency of answering: if
// it is slow, unreachable, or returns nonsense, the honest outcome is the
// answer we would have given yesterday, not an error page. So a failure is
// logged and dropped.
//
// Session chunks are deliberately not reranked. They come from a document the
// user just uploaded, and the spec puts them ahead of the KB on the grounds
// that uploading one is an explicit statement of what to answer from. That
// precedence is the user's, not the model's, so it is not a reranker's to
// overturn.
func (o *Orchestrator) reranked(ctx context.Context, question string, chunks []Chunk) []Chunk {
	if o.Reranker == nil || len(chunks) < 2 {
		return chunks
	}
	ranked, err := o.Reranker.Rerank(ctx, question, chunks)
	if err != nil {
		o.logger().Warn("rerank failed; falling back to retrieval order", "err", err)
		return chunks
	}
	if len(ranked) != len(chunks) {
		// Reorder guarantees this, so a mismatch means an implementation
		// that filtered rather than reordered. Refuse the result instead of
		// letting the pool quietly shrink.
		o.logger().Error("reranker changed the chunk count; ignoring its ranking",
			"in", len(chunks), "out", len(ranked))
		return chunks
	}
	return ranked
}

func (o *Orchestrator) maxContext() int {
	n := o.MaxContext
	if n <= 0 {
		n = o.TopK
	}
	if n < o.RerankN {
		n = o.RerankN
	}
	return n
}

// backfillSiblings re-admits chunks that come from a page already being cited
// but lost the cut at RerankN.
//
// Top-N-by-score is the wrong unit for a question about a whole document. Ask
// "what fields does AVR send?" and the five sections of the field-mapping page
// all score alike; RerankN=4 keeps four of them and drops one, and the answer
// silently omits an entire operation's fields without saying so. The user
// cannot tell a complete answer from a truncated one.
//
// So once selection by relevance is done, any sibling section of an
// already-selected page is pulled back in, in retrieval order, up to limit.
// Nothing new is retrieved and no page that lost on relevance is promoted:
// this only finishes documents the answer is already drawing on.
func backfillSiblings(chosen, all []Chunk, limit int) []Chunk {
	if len(chosen) >= limit {
		return chosen
	}
	// An empty PageID is "unknown page", not a page all such chunks share,
	// so it never groups.
	pages := make(map[string]struct{}, len(chosen))
	for _, c := range chosen {
		if c.PageID != "" {
			pages[c.PageID] = struct{}{}
		}
	}
	have := make(map[string]struct{}, len(chosen))
	for _, c := range chosen {
		have[c.ID] = struct{}{}
	}
	for _, c := range all {
		if len(chosen) >= limit {
			break
		}
		if _, ok := pages[c.PageID]; !ok {
			continue
		}
		if _, dup := have[c.ID]; dup {
			continue
		}
		have[c.ID] = struct{}{}
		chosen = append(chosen, c)
	}
	return chosen
}

// belowFloor reports whether retrieval found nothing worth grounding on.
func belowFloor(chosen []Chunk, floor float64) bool {
	if len(chosen) == 0 {
		return true
	}
	for _, c := range chosen {
		if c.Score >= floor {
			return false
		}
	}
	return true
}

// mark labels each retrieved chunk with its rank, whether it reached the
// prompt, and why. selected is how many of chosen were picked on relevance
// before sibling backfill appended the rest.
func mark(all, chosen []Chunk, selected int) []RetrievedChunk {
	reason := make(map[string]string, len(chosen))
	for i, c := range chosen {
		if i < selected {
			reason[c.ID] = "selected"
		} else {
			reason[c.ID] = "backfilled"
		}
	}
	out := make([]RetrievedChunk, 0, len(all))
	for i, c := range all {
		r, ok := reason[c.ID]
		if !ok {
			r = "dropped"
		}
		out = append(out, RetrievedChunk{Chunk: c, Used: ok, Rank: i + 1, Reason: r})
	}
	return out
}
