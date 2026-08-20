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
func (o *Orchestrator) Answer(ctx context.Context, question string, userGroups []string, session Retriever, out chan<- StreamEvent) error {
	var chosen []Chunk
	seen := map[string]struct{}{}

	if session != nil {
		sc, err := session.Search(ctx, question, userGroups, o.RerankN)
		if err == nil {
			for _, c := range sc {
				if _, dup := seen[c.ID]; dup {
					continue
				}
				seen[c.ID] = struct{}{}
				chosen = append(chosen, c)
				if len(chosen) >= o.RerankN {
					break
				}
			}
		}
	}

	if len(chosen) < o.RerankN {
		kb, err := o.Retriever.Search(ctx, question, userGroups, o.TopK)
		if err != nil {
			return fmt.Errorf("retrieve: %w", err)
		}
		for _, c := range kb {
			if _, dup := seen[c.ID]; dup {
				continue
			}
			seen[c.ID] = struct{}{}
			chosen = append(chosen, c)
			if len(chosen) >= o.RerankN {
				break
			}
		}
	}

	out <- StreamEvent{Type: "citation", Data: chosen}
	prompt := BuildPrompt(question, chosen)
	if err := o.LLM.Stream(ctx, prompt, out); err != nil {
		return fmt.Errorf("llm: %w", err)
	}
	out <- StreamEvent{Type: "done"}
	return nil
}
