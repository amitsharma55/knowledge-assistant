package rag

import (
	"context"
	"strings"
	"testing"

	"github.com/example/knowledge-assistant/internal/team"
)

type stubRetriever struct{ chunks []Chunk }

func (s stubRetriever) Search(_ context.Context, _ string, _ Scope, k int) ([]Chunk, error) {
	if k > len(s.chunks) {
		k = len(s.chunks)
	}
	return s.chunks[:k], nil
}

// capturingLLM records the prompt it was asked to complete.
type capturingLLM struct{ prompt string }

func (c *capturingLLM) Stream(_ context.Context, prompt string, out chan<- StreamEvent) error {
	c.prompt = prompt
	out <- StreamEvent{Type: "token", Data: "ok"}
	return nil
}

func fixtureScope(t *testing.T) Scope {
	t.Helper()
	reg := team.NewRegistry(team.DefaultInfos())
	coupa, err := reg.Parse("coupa")
	if err != nil {
		t.Fatalf("parse coupa: %v", err)
	}
	return NewScope(coupa, []team.Team{coupa}, nil)
}

func drain(t *testing.T, o *Orchestrator, llm *capturingLLM) []StreamEvent {
	t.Helper()
	out := make(chan StreamEvent, 64)
	go func() {
		if err := o.Answer(context.Background(), "invoice sync", fixtureScope(t), nil, out); err != nil {
			t.Errorf("Answer: %v", err)
		}
		close(out)
	}()
	var events []StreamEvent
	for e := range out {
		events = append(events, e)
	}
	return events
}

func sixChunks() []Chunk {
	var cs []Chunk
	for _, id := range []string{"c1", "c2", "c3", "c4", "c5", "c6"} {
		cs = append(cs, Chunk{ID: id, Team: "coupa", PageTitle: "Doc " + id, Text: "body of " + id})
	}
	return cs
}

func TestRetrievalEventMarksExactlyThePromptedChunks(t *testing.T) {
	llm := &capturingLLM{}
	o := &Orchestrator{Retriever: stubRetriever{sixChunks()}, LLM: llm, TopK: 6, RerankN: 4}

	events := drain(t, o, llm)

	var retrieved []RetrievedChunk
	for _, e := range events {
		if e.Type == "retrieval" {
			retrieved = e.Data.([]RetrievedChunk)
		}
	}
	if retrieved == nil {
		t.Fatal("no retrieval event was emitted")
	}
	if len(retrieved) != 6 {
		t.Fatalf("retrieval event carried %d chunks, want all 6 retrieved", len(retrieved))
	}

	var usedCount int
	for _, rc := range retrieved {
		if !rc.Used {
			if strings.Contains(llm.prompt, rc.Text) {
				t.Errorf("chunk %s is marked unused but appears in the prompt", rc.ID)
			}
			continue
		}
		usedCount++
		if !strings.Contains(llm.prompt, rc.Text) {
			t.Errorf("chunk %s is marked used but is absent from the prompt", rc.ID)
		}
	}
	if usedCount != 4 {
		t.Errorf("%d chunks marked used, want RerankN=4", usedCount)
	}
}

func TestRetrievalEventPrecedesCitations(t *testing.T) {
	llm := &capturingLLM{}
	o := &Orchestrator{Retriever: stubRetriever{sixChunks()}, LLM: llm, TopK: 6, RerankN: 4}

	var sawRetrieval bool
	for _, e := range drain(t, o, llm) {
		switch e.Type {
		case "retrieval":
			sawRetrieval = true
		case "citation":
			if !sawRetrieval {
				t.Fatal("citation event arrived before the retrieval event")
			}
		}
	}
}
