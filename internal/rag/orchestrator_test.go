package rag

import (
	"context"
	"fmt"
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

// TestKBRetrieverAlwaysQueriedEvenWhenSessionFillsChosen pins the fix for a
// spec gap: when the session retriever alone returns >= RerankN chunks, the
// primary (KB) retriever must still be queried so the retrieval event's
// `all` list is complete for the UI's context panel — the panel promises to
// show every chunk retrieved, with unused ones listed separately. Selection
// behaviour (`chosen`) must be unaffected: it stays exactly the session
// chunks.
func TestKBRetrieverAlwaysQueriedEvenWhenSessionFillsChosen(t *testing.T) {
	llm := &capturingLLM{}
	sessionChunks := []Chunk{
		{ID: "s1", Team: "coupa", PageTitle: "Upload", Text: "session chunk 1"},
		{ID: "s2", Team: "coupa", PageTitle: "Upload", Text: "session chunk 2"},
	}
	sess := stubRetriever{sessionChunks}
	o := &Orchestrator{Retriever: stubRetriever{sixChunks()}, LLM: llm, TopK: 6, RerankN: 2}

	out := make(chan StreamEvent, 64)
	go func() {
		if err := o.Answer(context.Background(), "invoice sync", fixtureScope(t), sess, out); err != nil {
			t.Errorf("Answer: %v", err)
		}
		close(out)
	}()

	var retrieved []RetrievedChunk
	var citations []Chunk
	for e := range out {
		switch e.Type {
		case "retrieval":
			retrieved = e.Data.([]RetrievedChunk)
		case "citation":
			citations = e.Data.([]Chunk)
		}
	}

	if len(citations) != 2 || citations[0].ID != "s1" || citations[1].ID != "s2" {
		t.Fatalf("chosen/citations changed: got %v, want exactly the two session chunks", citations)
	}

	if retrieved == nil {
		t.Fatal("no retrieval event was emitted")
	}
	var sawKBChunk, kbChunkMarkedUnused bool
	for _, rc := range retrieved {
		if rc.ID == "c1" {
			sawKBChunk = true
			kbChunkMarkedUnused = !rc.Used
		}
	}
	if !sawKBChunk {
		t.Fatal("primary retriever was never queried; retrieval event has zero KB chunks even though session chunks filled `chosen`")
	}
	if !kbChunkMarkedUnused {
		t.Error("KB chunk present but not marked unused")
	}
}

// erroringRetriever always fails; used to test that a session retriever
// failure degrades gracefully instead of aborting the whole answer.
type erroringRetriever struct{}

func (erroringRetriever) Search(_ context.Context, _ string, _ Scope, _ int) ([]Chunk, error) {
	return nil, fmt.Errorf("boom")
}

// TestSessionRetrieverErrorDoesNotAbortAnswer pins the deliberate asymmetry:
// a failed *session* retriever only drops uploaded-doc grounding (logged),
// while a failed *primary* retriever aborts the whole answer. Answer must
// still succeed and fall back to KB-only grounding.
func TestSessionRetrieverErrorDoesNotAbortAnswer(t *testing.T) {
	llm := &capturingLLM{}
	o := &Orchestrator{Retriever: stubRetriever{sixChunks()}, LLM: llm, TopK: 6, RerankN: 4}

	out := make(chan StreamEvent, 64)
	err := o.Answer(context.Background(), "invoice sync", fixtureScope(t), erroringRetriever{}, out)
	close(out)
	if err != nil {
		t.Fatalf("Answer returned an error for a failed session retriever, want graceful degradation: %v", err)
	}

	var sawDone bool
	for e := range out {
		if e.Type == "done" {
			sawDone = true
		}
	}
	if !sawDone {
		t.Error("no done event; answer did not complete despite session retriever failure")
	}
}

type stubCounter struct{ counts map[string]int }

func (s stubCounter) Count(_ context.Context, _ string, scope Scope, _ float64) (int, error) {
	return s.counts[scope.Team().Slug()], nil
}

func multiTeamScope(t *testing.T) Scope {
	t.Helper()
	reg := team.NewRegistry(team.DefaultInfos())
	star, _ := reg.Parse("star")
	hr, _ := reg.Parse("hr")
	return NewScope(star, []team.Team{star, hr}, nil)
}

func TestSuggestsOtherTeamsWhenNothingIsFound(t *testing.T) {
	llm := &capturingLLM{}
	o := &Orchestrator{
		Retriever: stubRetriever{nil},
		Counter:   stubCounter{counts: map[string]int{"hr": 3}},
		LLM:       llm, TopK: 6, RerankN: 4,
	}

	out := make(chan StreamEvent, 64)
	go func() {
		if err := o.Answer(context.Background(), "leave policy", multiTeamScope(t), nil, out); err != nil {
			t.Errorf("Answer: %v", err)
		}
		close(out)
	}()

	var suggestions []TeamSuggestion
	for e := range out {
		if e.Type == "suggestion" {
			suggestions = e.Data.([]TeamSuggestion)
		}
	}
	if len(suggestions) != 1 {
		t.Fatalf("got %d suggestions, want 1 (hr)", len(suggestions))
	}
	if suggestions[0].Team != "hr" || suggestions[0].Matches != 3 {
		t.Errorf("suggestion = %+v, want hr with 3 matches", suggestions[0])
	}
}

func TestSuggestionsCarryNoContentFromOtherTeams(t *testing.T) {
	llm := &capturingLLM{}
	o := &Orchestrator{
		Retriever: stubRetriever{nil},
		Counter:   stubCounter{counts: map[string]int{"hr": 3}},
		LLM:       llm, TopK: 6, RerankN: 4,
	}
	out := make(chan StreamEvent, 64)
	go func() {
		_ = o.Answer(context.Background(), "leave policy", multiTeamScope(t), nil, out)
		close(out)
	}()
	for e := range out {
		if e.Type != "suggestion" {
			continue
		}
		for _, s := range e.Data.([]TeamSuggestion) {
			// TeamSuggestion must expose counts only: no titles, no text.
			if s.Team == "" || s.Matches == 0 {
				t.Errorf("malformed suggestion %+v", s)
			}
		}
	}
}

func TestNoSuggestionsWhenResultsWereFound(t *testing.T) {
	llm := &capturingLLM{}
	o := &Orchestrator{
		Retriever: stubRetriever{sixChunks()},
		Counter:   stubCounter{counts: map[string]int{"hr": 3}},
		LLM:       llm, TopK: 6, RerankN: 4,
	}
	out := make(chan StreamEvent, 64)
	go func() {
		_ = o.Answer(context.Background(), "invoice sync", multiTeamScope(t), nil, out)
		close(out)
	}()
	for e := range out {
		if e.Type == "suggestion" {
			t.Error("suggestions emitted even though the active team had results")
		}
	}
}
