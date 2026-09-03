package rag

import (
	"context"
	"errors"
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
type capturingLLM struct{ prompt Prompt }

func (c *capturingLLM) Stream(_ context.Context, prompt Prompt, out chan<- StreamEvent) error {
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
		if err := o.Answer(context.Background(), "invoice sync", nil, fixtureScope(t), nil, out); err != nil {
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
			if strings.Contains(llm.prompt.User, rc.Text) {
				t.Errorf("chunk %s is marked unused but appears in the prompt", rc.ID)
			}
			continue
		}
		usedCount++
		if !strings.Contains(llm.prompt.User, rc.Text) {
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
		if err := o.Answer(context.Background(), "invoice sync", nil, fixtureScope(t), sess, out); err != nil {
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
	err := o.Answer(context.Background(), "invoice sync", nil, fixtureScope(t), erroringRetriever{}, out)
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
		if err := o.Answer(context.Background(), "leave policy", nil, multiTeamScope(t), nil, out); err != nil {
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
		_ = o.Answer(context.Background(), "leave policy", nil, multiTeamScope(t), nil, out)
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
		_ = o.Answer(context.Background(), "invoice sync", nil, multiTeamScope(t), nil, out)
		close(out)
	}()
	for e := range out {
		if e.Type == "suggestion" {
			t.Error("suggestions emitted even though the active team had results")
		}
	}
}

// fiveSectionPage mirrors the shape that produced a truncated answer: one
// document whose sections all score alike, more of them than RerankN.
func fiveSectionPage() []Chunk {
	sections := []string{"overview", "Invoice fields", "Purchase order fields", "Receipt fields", "Translation rules"}
	var cs []Chunk
	for i, sec := range sections {
		cs = append(cs, Chunk{
			ID: fmt.Sprintf("afm-%d", i), Team: "coupa",
			PageID: "avr-field-mapping", PageTitle: "AVR Field Mapping",
			SectionPath: sec, Text: "body of " + sec, Score: 0.9,
		})
	}
	return cs
}

func TestAnswerBackfillsSiblingSectionsPastRerankN(t *testing.T) {
	llm := &capturingLLM{}
	o := &Orchestrator{
		Retriever: stubRetriever{chunks: fiveSectionPage()},
		LLM:       llm, TopK: 8, RerankN: 4,
	}
	drain(t, o, llm)

	// Every section of the page the answer is drawing on must reach the
	// prompt. Dropping one silently omits a whole operation's fields.
	for _, c := range fiveSectionPage() {
		if !strings.Contains(llm.prompt.User, c.Text) {
			t.Errorf("section %q was cut from the prompt", c.SectionPath)
		}
	}
}

func TestBackfillDoesNotPromoteUnrelatedPages(t *testing.T) {
	chosen := []Chunk{{ID: "a1", PageID: "page-a"}}
	all := []Chunk{
		{ID: "a1", PageID: "page-a"},
		{ID: "b1", PageID: "page-b"}, // lost on relevance; must stay out
		{ID: "a2", PageID: "page-a"}, // sibling; must come back
	}
	got := backfillSiblings(chosen, all, 8)
	if len(got) != 2 || got[1].ID != "a2" {
		t.Fatalf("want [a1 a2], got %+v", ids(got))
	}
}

func TestBackfillRespectsLimit(t *testing.T) {
	chosen := []Chunk{{ID: "a1", PageID: "p"}}
	all := []Chunk{{ID: "a1", PageID: "p"}, {ID: "a2", PageID: "p"}, {ID: "a3", PageID: "p"}}
	if got := backfillSiblings(chosen, all, 2); len(got) != 2 {
		t.Errorf("want 2 chunks at limit 2, got %d", len(got))
	}
}

func ids(cs []Chunk) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

func TestMaxContextNeverBelowRerankN(t *testing.T) {
	// A deployment can set RerankN above MaxContext (this repo's own .env
	// runs RerankN=16 against a default MaxContext of 10). Backfill must not
	// silently become a no-op there.
	o := &Orchestrator{TopK: 20, RerankN: 16, MaxContext: 10}
	if got := o.maxContext(); got != 16 {
		t.Errorf("maxContext() = %d, want RerankN=16", got)
	}
	o = &Orchestrator{TopK: 20, RerankN: 16}
	if got := o.maxContext(); got != 20 {
		t.Errorf("maxContext() with MaxContext unset = %d, want TopK=20", got)
	}
}

type stubReranker struct {
	order  []string // chunk IDs, most relevant first
	err    error
	drop   bool // return fewer chunks than given
	calls  int
	gotQ   string
	gotIDs []string
}

func (s *stubReranker) Rerank(_ context.Context, q string, cs []Chunk) ([]Chunk, error) {
	s.calls++
	s.gotQ = q
	s.gotIDs = nil
	for _, c := range cs {
		s.gotIDs = append(s.gotIDs, c.ID)
	}
	if s.err != nil {
		return nil, s.err
	}
	byID := map[string]Chunk{}
	for _, c := range cs {
		byID[c.ID] = c
	}
	var out []Chunk
	for _, id := range s.order {
		if c, ok := byID[id]; ok {
			out = append(out, c)
		}
	}
	if s.drop && len(out) > 1 {
		out = out[:len(out)-1]
	}
	return out, nil
}

func promptOrder(t *testing.T, prompt string, ids []string) []string {
	t.Helper()
	var seen []string
	for _, id := range ids {
		if i := strings.Index(prompt, "body of "+id); i >= 0 {
			seen = append(seen, id)
		}
	}
	return seen
}

func TestRerankerDecidesWhatReachesThePrompt(t *testing.T) {
	// c6 is last out of retrieval and would never survive RerankN=4.
	rr := &stubReranker{order: []string{"c6", "c5", "c4", "c3", "c2", "c1"}}
	llm := &capturingLLM{}
	o := &Orchestrator{
		Retriever: stubRetriever{chunks: sixChunks()},
		LLM:       llm, TopK: 6, RerankN: 4, MaxContext: 4, Reranker: rr,
	}
	drain(t, o, llm)

	if rr.calls != 1 {
		t.Fatalf("reranker called %d times, want 1", rr.calls)
	}
	if rr.gotQ != "invoice sync" {
		t.Errorf("reranker got question %q", rr.gotQ)
	}
	if len(rr.gotIDs) != 6 {
		t.Errorf("reranker got %d chunks, want the whole pool of 6", len(rr.gotIDs))
	}
	if !strings.Contains(llm.prompt.User, "body of c6") {
		t.Error("c6 was ranked first but never reached the prompt")
	}
	if strings.Contains(llm.prompt.User, "body of c1") {
		t.Error("c1 was ranked last but still reached the prompt")
	}
}

func TestRerankFailureFallsBackToRetrievalOrder(t *testing.T) {
	// A reranker that is down, slow or broken must cost us ordering, not
	// the answer.
	for name, rr := range map[string]*stubReranker{
		"error":         {err: errors.New("connection refused")},
		"dropped chunk": {order: []string{"c1", "c2", "c3", "c4", "c5", "c6"}, drop: true},
	} {
		t.Run(name, func(t *testing.T) {
			llm := &capturingLLM{}
			o := &Orchestrator{
				Retriever: stubRetriever{chunks: sixChunks()},
				LLM:       llm, TopK: 6, RerankN: 4, MaxContext: 4, Reranker: rr,
			}
			drain(t, o, llm)
			got := promptOrder(t, llm.prompt.User, []string{"c1", "c2", "c3", "c4", "c5", "c6"})
			want := []string{"c1", "c2", "c3", "c4"}
			if len(got) != len(want) {
				t.Fatalf("prompt held %v, want retrieval order %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("prompt held %v, want retrieval order %v", got, want)
				}
			}
		})
	}
}

func TestNoRerankerLeavesRetrievalOrder(t *testing.T) {
	llm := &capturingLLM{}
	o := &Orchestrator{
		Retriever: stubRetriever{chunks: sixChunks()},
		LLM:       llm, TopK: 6, RerankN: 4, MaxContext: 4,
	}
	drain(t, o, llm)
	if !strings.Contains(llm.prompt.User, "body of c1") {
		t.Error("c1 should reach the prompt when no reranker is configured")
	}
}

type stubRewriter struct {
	out     string
	err     error
	calls   int
	gotHist []Turn
}

func (s *stubRewriter) Rewrite(_ context.Context, _ string, h []Turn) (string, error) {
	s.calls++
	s.gotHist = h
	return s.out, s.err
}

// recordingRetriever captures the query it was searched with.
type recordingRetriever struct {
	chunks []Chunk
	query  string
}

func (r *recordingRetriever) Search(_ context.Context, q string, _ Scope, k int) ([]Chunk, error) {
	r.query = q
	if k > len(r.chunks) {
		k = len(r.chunks)
	}
	return r.chunks[:k], nil
}

func answerWith(t *testing.T, o *Orchestrator, q string, hist []Turn) {
	t.Helper()
	out := make(chan StreamEvent, 64)
	go func() {
		if err := o.Answer(context.Background(), q, hist, fixtureScope(t), nil, out); err != nil {
			t.Errorf("Answer: %v", err)
		}
		close(out)
	}()
	for range out {
	}
}

func TestFollowUpRetrievesOnTheRewrittenQuery(t *testing.T) {
	rw := &stubRewriter{out: "AVR field mapping invoice purchase order receipt fields"}
	ret := &recordingRetriever{chunks: sixChunks()}
	llm := &capturingLLM{}
	o := &Orchestrator{Retriever: ret, LLM: llm, TopK: 6, RerankN: 4, Rewriter: rw}

	hist := []Turn{
		{Role: "user", Content: "what data fields involved in it?"},
		{Role: "assistant", Content: "Which integration are you asking about?"},
	}
	answerWith(t, o, "I am asking about AVR", hist)

	if rw.calls != 1 {
		t.Fatalf("rewriter called %d times, want 1", rw.calls)
	}
	if len(rw.gotHist) != 2 {
		t.Errorf("rewriter got %d turns of history, want 2", len(rw.gotHist))
	}
	if ret.query != rw.out {
		t.Errorf("retrieved on %q, want the rewritten query %q", ret.query, rw.out)
	}
	// The user still gets an answer to what they typed, not to the rewrite.
	if !strings.Contains(llm.prompt.User, "Question: I am asking about AVR") {
		t.Error("the prompt should carry the question as asked, not the rewrite")
	}
}

func TestFirstTurnIsNeverRewritten(t *testing.T) {
	rw := &stubRewriter{out: "should not be used"}
	ret := &recordingRetriever{chunks: sixChunks()}
	o := &Orchestrator{Retriever: ret, LLM: &capturingLLM{}, TopK: 6, RerankN: 4, Rewriter: rw}

	answerWith(t, o, "what fields does AVR send", nil)

	if rw.calls != 0 {
		t.Errorf("rewriter ran on a first turn (%d calls); it has nothing to resolve against", rw.calls)
	}
	if ret.query != "what fields does AVR send" {
		t.Errorf("retrieved on %q, want the question as asked", ret.query)
	}
}

func TestRewriteFailureRetrievesOnTheQuestionAsAsked(t *testing.T) {
	for name, rw := range map[string]*stubRewriter{
		"error": {err: errors.New("ollama down")},
		"empty": {out: "   "},
	} {
		t.Run(name, func(t *testing.T) {
			ret := &recordingRetriever{chunks: sixChunks()}
			o := &Orchestrator{Retriever: ret, LLM: &capturingLLM{}, TopK: 6, RerankN: 4, Rewriter: rw}
			hist := []Turn{{Role: "user", Content: "earlier"}, {Role: "assistant", Content: "reply"}}
			answerWith(t, o, "I am asking about AVR", hist)
			if ret.query != "I am asking about AVR" {
				t.Errorf("retrieved on %q, want a fallback to the question as asked", ret.query)
			}
		})
	}
}

func TestHistoryReachesThePrompt(t *testing.T) {
	llm := &capturingLLM{}
	o := &Orchestrator{Retriever: stubRetriever{chunks: sixChunks()}, LLM: llm, TopK: 6, RerankN: 4}
	hist := []Turn{
		{Role: "user", Content: "what data fields involved in it?"},
		{Role: "assistant", Content: "Which integration are you asking about?"},
	}
	answerWith(t, o, "I am asking about AVR", hist)

	if len(llm.prompt.History) != 2 {
		t.Fatalf("prompt carried %d turns of history, want 2", len(llm.prompt.History))
	}
	if llm.prompt.History[0].Content != hist[0].Content {
		t.Error("history reached the prompt out of order or altered")
	}
}

// multiQueryRetriever records every query it was searched with and returns a
// distinct chunk per query, so a merge can be observed.
type multiQueryRetriever struct {
	queries []string
	byQuery map[string][]Chunk
}

func (r *multiQueryRetriever) Search(_ context.Context, q string, _ Scope, _ int) ([]Chunk, error) {
	r.queries = append(r.queries, q)
	return r.byQuery[q], nil
}

func TestDualRetrievalSearchesBothAndMerges(t *testing.T) {
	orig := Chunk{ID: "from-original", PageID: "p1", Text: "body of original"}
	rew := Chunk{ID: "from-rewrite", PageID: "p2", Text: "body of rewrite"}
	shared := Chunk{ID: "shared", PageID: "p3", Text: "body of shared"}

	ret := &multiQueryRetriever{byQuery: map[string][]Chunk{
		"I am asking about AVR": {orig, shared},
		"AVR field mapping":     {rew, shared},
	}}
	rw := &stubRewriter{out: "AVR field mapping"}
	llm := &capturingLLM{}
	o := &Orchestrator{
		Retriever: ret, LLM: llm, TopK: 10, RerankN: 10, MaxContext: 10,
		Rewriter: rw, DualRetrieval: true,
	}
	answerWith(t, o, "I am asking about AVR", []Turn{{Role: "user", Content: "earlier"}})

	if len(ret.queries) != 2 {
		t.Fatalf("ran %d searches (%v), want one per query", len(ret.queries), ret.queries)
	}
	if ret.queries[0] != "I am asking about AVR" {
		t.Errorf("first search was %q; the question as asked must be searched too", ret.queries[0])
	}
	// A bad rewrite must not be able to lose what the original found.
	for _, want := range []string{"body of original", "body of rewrite", "body of shared"} {
		if !strings.Contains(llm.prompt.User, want) {
			t.Errorf("%q is missing from the merged pool", want)
		}
	}
	// The shared chunk came back from both queries; it must appear once.
	if n := strings.Count(llm.prompt.User, "body of shared"); n != 1 {
		t.Errorf("shared chunk appears %d times, want 1 (dedupe by id)", n)
	}
}

func TestDualRetrievalRewritesEvenTheFirstTurn(t *testing.T) {
	// Safe here in a way it is not in single mode: the original is searched
	// alongside, so a rewrite can only add.
	rw := &stubRewriter{out: "AVR invoice field mapping"}
	ret := &multiQueryRetriever{byQuery: map[string][]Chunk{}}
	o := &Orchestrator{Retriever: ret, LLM: &capturingLLM{}, TopK: 6, RerankN: 4,
		Rewriter: rw, DualRetrieval: true}

	answerWith(t, o, "what fields", nil)

	if rw.calls != 1 {
		t.Errorf("rewriter ran %d times on a first turn, want 1 in dual mode", rw.calls)
	}
	if len(ret.queries) != 2 {
		t.Errorf("ran %d searches (%v), want 2", len(ret.queries), ret.queries)
	}
}

func TestDualRetrievalSearchesOnceWhenTheRewriteIsANoOp(t *testing.T) {
	rw := &stubRewriter{out: "  what fields  "} // same question, padded
	ret := &multiQueryRetriever{byQuery: map[string][]Chunk{}}
	o := &Orchestrator{Retriever: ret, LLM: &capturingLLM{}, TopK: 6, RerankN: 4,
		Rewriter: rw, DualRetrieval: true}

	answerWith(t, o, "what fields", nil)

	if len(ret.queries) != 1 {
		t.Errorf("ran %d searches (%v); an unchanged rewrite needs no second search", len(ret.queries), ret.queries)
	}
}

func TestRerankJudgesAgainstTheResolvedQuery(t *testing.T) {
	rr := &stubReranker{order: []string{"c1"}}
	rw := &stubRewriter{out: "AVR field mapping"}
	ret := &multiQueryRetriever{byQuery: map[string][]Chunk{
		"I am asking about AVR": {{ID: "c1", PageID: "p", Text: "body of c1"}},
		"AVR field mapping":     {{ID: "c2", PageID: "p", Text: "body of c2"}},
	}}
	o := &Orchestrator{Retriever: ret, LLM: &capturingLLM{}, TopK: 6, RerankN: 4,
		Rewriter: rw, Reranker: rr, DualRetrieval: true}

	answerWith(t, o, "I am asking about AVR", []Turn{{Role: "user", Content: "earlier"}})

	if rr.gotQ != "AVR field mapping" {
		t.Errorf("reranked against %q; a bare follow-up is nothing to judge relevance against", rr.gotQ)
	}
}

func TestRetrievalEventExplainsWhyEachChunkWasKeptOrCut(t *testing.T) {
	// One page with five sections, RerankN=2, so two are selected on
	// relevance and the rest come back as siblings until MaxContext.
	o := &Orchestrator{
		Retriever: stubRetriever{chunks: fiveSectionPage()},
		LLM:       &capturingLLM{}, TopK: 5, RerankN: 2, MaxContext: 4,
	}
	out := make(chan StreamEvent, 64)
	go func() {
		if err := o.Answer(context.Background(), "avr fields", nil, fixtureScope(t), nil, out); err != nil {
			t.Errorf("Answer: %v", err)
		}
		close(out)
	}()
	var got []RetrievedChunk
	for e := range out {
		if e.Type == "retrieval" {
			got = e.Data.([]RetrievedChunk)
		}
	}
	if len(got) != 5 {
		t.Fatalf("retrieval event carried %d chunks, want 5", len(got))
	}

	counts := map[string]int{}
	for i, rc := range got {
		counts[rc.Reason]++
		if rc.Rank != i+1 {
			t.Errorf("chunk %d has rank %d; rank must be its position after reranking", i+1, rc.Rank)
		}
		if rc.Used != (rc.Reason != "dropped") {
			t.Errorf("chunk %d: used=%v contradicts reason %q", i+1, rc.Used, rc.Reason)
		}
	}
	if counts["selected"] != 2 {
		t.Errorf("%d chunks marked selected, want RerankN=2", counts["selected"])
	}
	if counts["backfilled"] != 2 {
		t.Errorf("%d chunks marked backfilled, want 2 (MaxContext 4 minus RerankN 2)", counts["backfilled"])
	}
	if counts["dropped"] != 1 {
		t.Errorf("%d chunks marked dropped, want 1", counts["dropped"])
	}
}
