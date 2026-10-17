package rag

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestIsCreateTicketQuery(t *testing.T) {
	yes := []string{
		"create a ticket for my group",
		"create a ticket: email server down",
		"open a ticket please",
		"raise an incident",
		"please file a new ticket",
		"submit a ticket",
	}
	no := []string{
		"any open tickets?",                       // read: "open tickets", no article
		"latest open ticket for Team star",        // read
		"show me open tickets",                    // read
		"how many tickets opened since last week", // count
		"how does AVR send fields to Coupa?",
		"create a new dashboard", // no ticket/incident
	}
	for _, q := range yes {
		if !isCreateTicketQuery(q) {
			t.Errorf("isCreateTicketQuery(%q) = false, want true", q)
		}
	}
	for _, q := range no {
		if isCreateTicketQuery(q) {
			t.Errorf("isCreateTicketQuery(%q) = true, want false", q)
		}
	}
}

func TestCreateInProgress(t *testing.T) {
	yes := [][]Turn{
		{{Role: "user", Content: "create a ticket"}, {Role: "assistant", Content: issuePrompt}},
		{{Role: "assistant", Content: severityPrompt}},
		{{Role: "assistant", Content: draftText("star", "email down", "2")}},
	}
	no := [][]Turn{
		nil,
		{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "Hello!"}},
		{{Role: "assistant", Content: "Filed INC0099999 — http://x/incident.do?sys_id=1"}},
	}
	for i, h := range yes {
		if !createInProgress(h) {
			t.Errorf("createInProgress(yes[%d]) = false, want true", i)
		}
	}
	for i, h := range no {
		if createInProgress(h) {
			t.Errorf("createInProgress(no[%d]) = true, want false", i)
		}
	}
}

func TestPriorityFor(t *testing.T) {
	cases := map[string]string{
		"critical": "1", "High": "2", "it's high": "2",
		"moderate": "3", "medium": "3", "low": "4",
	}
	for word, want := range cases {
		if got, ok := priorityFor(word); !ok || got != want {
			t.Errorf("priorityFor(%q) = (%q, %v), want (%q, true)", word, got, ok, want)
		}
	}
	if _, ok := priorityFor("urgent-ish"); ok {
		t.Error("priorityFor(unmapped) ok = true, want false")
	}
}

func TestDraftFromHistoryPhases(t *testing.T) {
	// Progressive history for a normal (no inline) flow.
	afterIssuePrompt := []Turn{
		{Role: "user", Content: "create a ticket"},
		{Role: "assistant", Content: issuePrompt},
	}
	afterSeverityPrompt := append(append([]Turn{}, afterIssuePrompt...),
		Turn{Role: "user", Content: "email server down"},
		Turn{Role: "assistant", Content: severityPrompt},
	)
	afterDraft := append(append([]Turn{}, afterSeverityPrompt...),
		Turn{Role: "user", Content: "high"},
		Turn{Role: "assistant", Content: draftText("coupa", "email server down", "2")},
	)

	cases := []struct {
		name     string
		history  []Turn
		message  string
		phase    string
		issue    string
		priority string
	}{
		{"fresh no inline", nil, "create a ticket for my group", "issue", "", ""},
		{"fresh inline", nil, "create a ticket: printer broken", "severity", "", ""},
		{"issue reply", afterIssuePrompt, "email server down", "severity", "", ""},
		{"severity mapped", afterSeverityPrompt, "high", "draft", "email server down", "2"},
		{"severity unmapped re-ask", afterSeverityPrompt, "sorta bad", "severity", "", ""},
		{"confirm", afterDraft, "confirm", "confirm", "email server down", "2"},
	}
	for _, tc := range cases {
		gotPhase, gotIssue, gotPrio := draftFromHistory(tc.history, tc.message)
		if gotPhase != tc.phase || gotIssue != tc.issue || gotPrio != tc.priority {
			t.Errorf("%s: draftFromHistory = (%q,%q,%q), want (%q,%q,%q)",
				tc.name, gotPhase, gotIssue, gotPrio, tc.phase, tc.issue, tc.priority)
		}
	}
}

func TestDraftFromHistoryInlineIssueSurvivesToConfirm(t *testing.T) {
	// The inline "create a ticket: X" turn is the one answered with the severity
	// prompt, so X must be recovered (prefix stripped) at confirm.
	history := []Turn{
		{Role: "user", Content: "create a ticket: vpn is down"},
		{Role: "assistant", Content: severityPrompt},
		{Role: "user", Content: "critical"},
		{Role: "assistant", Content: draftText("coupa", "vpn is down", "1")},
	}
	phase, issue, prio := draftFromHistory(history, "yes")
	if phase != "confirm" || issue != "vpn is down" || prio != "1" {
		t.Errorf("draftFromHistory = (%q,%q,%q), want (confirm, vpn is down, 1)", phase, issue, prio)
	}
}

// runCreate drives one turn through Answer against the coupa fixtureScope with
// the given history, returning the streamed token text.
func runCreate(t *testing.T, o *Orchestrator, message string, history []Turn) string {
	t.Helper()
	out := make(chan StreamEvent, 64)
	errc := make(chan error, 1)
	go func() {
		errc <- o.Answer(context.Background(), message, history, fixtureScope(t), nil, out)
		close(out)
	}()
	var events []StreamEvent
	for e := range out {
		events = append(events, e)
	}
	if err := <-errc; err != nil {
		t.Fatalf("Answer: %v", err)
	}
	return tokenText(events)
}

func TestCreateFlowWalkedTurnByTurn(t *testing.T) {
	src := &stubTicketSource{created: Ticket{Number: "INC0077777", URL: "http://sn/incident.do?sys_id=zz"}}
	// A Retriever is needed for the final stray-"yes" turn, which is not a
	// create query and so falls through to normal retrieval.
	o := &Orchestrator{Retriever: stubRetriever{sixChunks()}, LLM: &capturingLLM{}, Tickets: src, TopK: 6, RerankN: 4}

	var history []Turn
	add := func(userMsg, assistantMsg string) {
		history = append(history,
			Turn{Role: "user", Content: userMsg},
			Turn{Role: "assistant", Content: assistantMsg})
	}

	// Turn 1: fresh request → issue prompt.
	got := runCreate(t, o, "create a ticket for my group", history)
	if got != issuePrompt {
		t.Fatalf("turn 1 = %q, want the issue prompt", got)
	}
	add("create a ticket for my group", got)

	// Turn 2: issue → severity prompt.
	got = runCreate(t, o, "email server is down", history)
	if got != severityPrompt {
		t.Fatalf("turn 2 = %q, want the severity prompt", got)
	}
	add("email server is down", got)

	// Turn 3: severity → draft.
	got = runCreate(t, o, "high", history)
	if !strings.HasPrefix(got, draftPrefix) || !strings.Contains(got, "email server is down") || !strings.Contains(got, "High") {
		t.Fatalf("turn 3 = %q, want a draft naming the issue and priority", got)
	}
	add("high", got)

	// No write may have happened before confirm.
	if src.createCalls != 0 {
		t.Fatalf("Create called %d times before confirm, want 0", src.createCalls)
	}

	// Turn 4: confirm → file, exactly once.
	got = runCreate(t, o, "confirm", history)
	if src.createCalls != 1 {
		t.Fatalf("Create called %d times, want exactly 1", src.createCalls)
	}
	if src.gotDraft.ShortDescription != "email server is down" || src.gotDraft.Priority != "2" {
		t.Errorf("filed draft = %+v, want the collected issue and priority 2", src.gotDraft)
	}
	if !strings.Contains(got, "INC0077777") || !strings.Contains(got, "incident.do?sys_id=zz") {
		t.Errorf("confirm reply = %q, want the filed number and URL", got)
	}
	add("confirm", got)

	// A later stray "yes" must not re-file: the last assistant turn is "Filed…",
	// not a draft, so it's a fresh turn (falls through to retrieval), not a
	// confirm of the old draft.
	_ = runCreate(t, o, "yes", history)
	if src.createCalls != 1 {
		t.Errorf("stray yes re-filed; Create calls = %d, want 1", src.createCalls)
	}
}

func TestCreateFlowCancel(t *testing.T) {
	src := &stubTicketSource{}
	o := &Orchestrator{LLM: &capturingLLM{}, Tickets: src}
	history := []Turn{
		{Role: "user", Content: "email down"},
		{Role: "assistant", Content: draftText("coupa", "email down", "2")},
	}
	got := runCreate(t, o, "cancel", history)
	if src.createCalls != 0 {
		t.Errorf("cancel filed a ticket; Create calls = %d, want 0", src.createCalls)
	}
	if !strings.Contains(strings.ToLower(got), "cancel") {
		t.Errorf("cancel reply = %q, want a cancellation message", got)
	}
}

func TestCreateFlowUnmappedSeverityReAsks(t *testing.T) {
	src := &stubTicketSource{}
	o := &Orchestrator{LLM: &capturingLLM{}, Tickets: src}
	history := []Turn{
		{Role: "user", Content: "create a ticket"},
		{Role: "assistant", Content: issuePrompt},
		{Role: "user", Content: "email down"},
		{Role: "assistant", Content: severityPrompt},
	}
	got := runCreate(t, o, "kinda bad", history)
	if got != severityPrompt {
		t.Errorf("unmapped severity = %q, want the severity prompt re-asked", got)
	}
}

func TestCreateFlowNotConfigured(t *testing.T) {
	src := &stubTicketSource{createErr: ErrTeamNotConfigured}
	o := &Orchestrator{LLM: &capturingLLM{}, Tickets: src}
	history := []Turn{
		{Role: "user", Content: "email down"},
		{Role: "assistant", Content: draftText("coupa", "email down", "2")},
	}
	got := runCreate(t, o, "confirm", history)
	if !strings.Contains(strings.ToLower(got), "isn't configured") && !strings.Contains(strings.ToLower(got), "not configured") {
		t.Errorf("not-configured reply = %q, want a configuration message", got)
	}
}

func TestCreateFlowTransportError(t *testing.T) {
	src := &stubTicketSource{createErr: errors.New("boom")}
	o := &Orchestrator{LLM: &capturingLLM{}, Tickets: src}
	history := []Turn{
		{Role: "user", Content: "email down"},
		{Role: "assistant", Content: draftText("coupa", "email down", "2")},
	}
	got := runCreate(t, o, "confirm", history)
	if !strings.Contains(strings.ToLower(got), "couldn't file") {
		t.Errorf("transport error reply = %q, want a try-again message", got)
	}
}

func TestCreateGateDisabledWhenNoTicketSource(t *testing.T) {
	// With Tickets nil, a create phrase must fall through to retrieval, not panic.
	o := &Orchestrator{Retriever: stubRetriever{sixChunks()}, LLM: &capturingLLM{}, TopK: 6, RerankN: 4}
	events := answerEvents(t, o, "create a ticket for my group")
	if !hasType(events, "citation") {
		t.Error("create phrase with Tickets nil did not go through retrieval")
	}
}

func TestCreatePhraseDoesNotReachReadBranch(t *testing.T) {
	src := &stubTicketSource{tickets: []Ticket{{Number: "INC1"}}}
	o := &Orchestrator{LLM: &capturingLLM{}, Tickets: src}
	_ = runCreate(t, o, "create a ticket for my group", nil)
	if src.called {
		t.Error("create phrase reached the read branch (LatestOpen was called)")
	}
}

func TestReadPhraseDoesNotReachCreateBranch(t *testing.T) {
	src := &stubTicketSource{tickets: []Ticket{{Number: "INC1", ShortDescription: "VPN down"}}}
	o := &Orchestrator{LLM: &capturingLLM{}, Tickets: src}
	_ = runCreate(t, o, "any open tickets?", nil)
	if src.createCalls != 0 {
		t.Error("read phrase reached the create branch")
	}
	if !src.called {
		t.Error("read phrase did not reach the read branch")
	}
}
