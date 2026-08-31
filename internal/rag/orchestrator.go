package rag

import (
	"context"
	"fmt"
)

type Orchestrator struct {
	Retriever Retriever
	LLM       LLM
	TopK      int
	RerankN   int
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
		if err == nil {
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

	if len(chosen) < o.RerankN {
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
