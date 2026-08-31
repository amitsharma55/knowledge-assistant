package rag

import (
	"context"
	"fmt"
	"log/slog"
)

type Orchestrator struct {
	Retriever      Retriever
	LLM            LLM
	TopK           int
	RerankN        int
	Counter        Counter
	RelevanceFloor float64
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
func (o *Orchestrator) Answer(ctx context.Context, question string, scope Scope, session Retriever, out chan<- StreamEvent) error {
	var chosen []Chunk
	var all []Chunk
	seen := map[string]struct{}{}
	seenAll := map[string]struct{}{}

	if session != nil {
		sc, err := session.Search(ctx, question, scope, o.RerankN)
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
	kb, err := o.Retriever.Search(ctx, question, scope, o.TopK)
	if err != nil {
		return fmt.Errorf("retrieve: %w", err)
	}
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
			n, err := o.Counter.Count(ctx, question, probe, o.RelevanceFloor)
			if err != nil || n == 0 {
				continue
			}
			suggestions = append(suggestions, TeamSuggestion{Team: other.Slug(), Matches: n})
		}
		if len(suggestions) > 0 {
			out <- StreamEvent{Type: "suggestion", Data: suggestions}
		}
	}

	out <- StreamEvent{Type: "retrieval", Data: mark(all, chosen)}
	out <- StreamEvent{Type: "citation", Data: chosen}
	prompt := BuildPrompt(question, chosen)
	if err := o.LLM.Stream(ctx, prompt, out); err != nil {
		return fmt.Errorf("llm: %w", err)
	}
	out <- StreamEvent{Type: "done"}
	return nil
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

// mark labels each retrieved chunk with whether it reached the prompt.
func mark(all, chosen []Chunk) []RetrievedChunk {
	used := make(map[string]struct{}, len(chosen))
	for _, c := range chosen {
		used[c.ID] = struct{}{}
	}
	out := make([]RetrievedChunk, 0, len(all))
	for _, c := range all {
		_, ok := used[c.ID]
		out = append(out, RetrievedChunk{Chunk: c, Used: ok})
	}
	return out
}
