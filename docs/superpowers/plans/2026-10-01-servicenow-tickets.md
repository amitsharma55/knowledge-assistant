# ServiceNow Real-Time Tickets Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user ask in chat for their team's open ServiceNow tickets and get a short, LLM-written summary of live tickets — never answered from the RAG corpus.

**Architecture:** A dedicated intent gate in `rag.Orchestrator.Answer`, right after the greeting gate, fires on every turn. A heuristic fast-path (`isTicketQuery`) catches common phrasing; an optional `Classifier` LLM call catches the rest. On ticket intent the orchestrator fetches via a decoupled `TicketSource` interface, summarizes with the existing LLM, and never falls back to retrieval. A chat-api adapter resolves `team.Team` → ServiceNow `assignment_group` and calls a thin basic-auth `servicenow.Client`.

**Tech Stack:** Go (stdlib `net/http`, `encoding/json`), ServiceNow Table API, existing `internal/rag`, `internal/team`, AWS Secrets Manager via `services/chat-api/internal/secrets`.

**Spec:** `docs/superpowers/specs/2026-10-01-servicenow-tickets-design.md`

## Global Constraints

- Go must be gofmt-clean; a hook formats after Edit/Write, but run `gofmt -w` after any shell-driven Go change.
- Wrap errors with a package prefix: `fmt.Errorf("servicenow: context: %w", err)`. Operator-facing errors say how to fix.
- HTTP clients are tested with `httptest` servers; collaborators are hand-written `stubX` types in `*_test.go`.
- Team is never inferred from message content; it comes from `X-Team` + `team.Authorize`. The intent gate decides *intent only*, never *which team*.
- Basic auth only for ServiceNow. Local creds (URL, user, password) from `.env`; in AWS the password comes from Secrets Manager via `KA_SERVICENOW_SECRET_ID`.
- The feature is gated by `KA_SERVICENOW_MODE` (default `off`); when off, `Orchestrator.Tickets` is nil and ticket phrasing must fall through to normal RAG.

## Review Focus

- Group names with spaces (e.g. `Star Support`) must be URL-encoded in `sysparm_query` — pinned by a Task 1 test asserting the encoded request URL.
- ServiceNow returns a non-2xx (401 bad auth / 500) → honest error surfaced, no panic — pinned by Task 1 (client returns error) and Task 5 (orchestrator streams the "can't reach" status).
- Classifier error/timeout must not fail the turn — pinned by Task 5: stub Classifier returning an error falls through to the RAG path.
- Ticket phrasing while the feature is off (`Tickets == nil`) must run normal RAG, not hijack the turn — pinned by Task 5: `isTicketQuery` true + nil `Tickets` → retriever is called.
- A team configured with an empty `ServiceNowGroup` → "not configured" status, never a ServiceNow call with an empty group — pinned by Task 6 (adapter returns `ErrTeamNotConfigured`) and Task 5 (status streamed).
- Malformed `KA_TEAM_METADATA` JSON must fail fast at startup, not silently disable tickets — pinned by Task 2 (`ParseMetadata` returns an error on bad JSON) and Task 7 (startup exits non-zero on that error).

---

### Task 1: `internal/servicenow` client

**Files:**
- Create: `internal/servicenow/servicenow.go`
- Test: `internal/servicenow/servicenow_test.go`

**Interfaces:**
- Consumes: nothing (leaf package, stdlib only).
- Produces:
  - `type Incident struct { Number, ShortDescription, State, Priority, UpdatedOn, URL string }`
  - `type Client struct { BaseURL, User, Password string; HTTP *http.Client }`
  - `func (c Client) LatestOpen(ctx context.Context, group string, limit int) ([]Incident, error)`

- [ ] **Step 1: Write the failing test**

```go
package servicenow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLatestOpenQueryAndMapping(t *testing.T) {
	var gotQuery, gotAuthUser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("sysparm_query")
		u, _, _ := r.BasicAuth()
		gotAuthUser = u
		_ = json.NewEncoder(w).Encode(map[string]any{"result": []map[string]string{{
			"number": "INC0012345", "short_description": "VPN down",
			"state": "In Progress", "priority": "2 - High",
			"sys_updated_on": "2026-10-01 14:05:00", "sys_id": "abc123",
		}}})
	}))
	defer srv.Close()

	c := Client{BaseURL: srv.URL, User: "svc", Password: "pw", HTTP: srv.Client()}
	got, err := c.LatestOpen(context.Background(), "Star Support", 5)
	if err != nil {
		t.Fatalf("LatestOpen: %v", err)
	}
	// Group with a space must be carried intact in the decoded query.
	if !strings.Contains(gotQuery, "assignment_group.name=Star Support") {
		t.Errorf("sysparm_query = %q, want it to contain the group name verbatim", gotQuery)
	}
	if !strings.Contains(gotQuery, "active=true") || !strings.Contains(gotQuery, "ORDERBYDESCsys_updated_on") {
		t.Errorf("sysparm_query = %q, want active=true and ORDERBYDESCsys_updated_on", gotQuery)
	}
	if gotAuthUser != "svc" {
		t.Errorf("basic-auth user = %q, want svc", gotAuthUser)
	}
	if len(got) != 1 {
		t.Fatalf("got %d incidents, want 1", len(got))
	}
	if got[0].Number != "INC0012345" || got[0].State != "In Progress" {
		t.Errorf("mapped incident = %+v", got[0])
	}
	if !strings.HasPrefix(got[0].URL, srv.URL) || !strings.Contains(got[0].URL, "abc123") {
		t.Errorf("incident URL = %q, want it to point at sys_id abc123", got[0].URL)
	}
}

func TestLatestOpenErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, User: "x", Password: "y", HTTP: srv.Client()}
	if _, err := c.LatestOpen(context.Background(), "Star Support", 5); err == nil {
		t.Fatal("LatestOpen on 401 = nil error, want an error")
	}
}

func TestLatestOpenEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"result": []map[string]string{}})
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, User: "x", Password: "y", HTTP: srv.Client()}
	got, err := c.LatestOpen(context.Background(), "Star Support", 5)
	if err != nil {
		t.Fatalf("LatestOpen: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d incidents, want 0", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/servicenow/`
Expected: FAIL — package/`Client` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// Package servicenow is a thin, read-only client for the ServiceNow Table API.
// It fetches open incidents for an assignment group; it never writes.
package servicenow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Incident is the subset of a ServiceNow incident this app summarizes.
type Incident struct {
	Number           string
	ShortDescription string
	State            string
	Priority         string
	UpdatedOn        string
	URL              string
}

// Client talks to one ServiceNow instance with basic auth.
type Client struct {
	BaseURL  string
	User     string
	Password string
	HTTP     *http.Client
}

func (c Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// LatestOpen returns up to limit active incidents for the assignment group,
// newest-updated first. limit <= 0 defaults to 5.
func (c Client) LatestOpen(ctx context.Context, group string, limit int) ([]Incident, error) {
	if limit <= 0 {
		limit = 5
	}
	u, err := url.Parse(strings.TrimSuffix(c.BaseURL, "/") + "/api/now/table/incident")
	if err != nil {
		return nil, fmt.Errorf("servicenow: parse base url %q: %w", c.BaseURL, err)
	}
	q := url.Values{}
	// Encoded-query grammar: name=value pairs joined by ^, ORDERBYDESC<field>
	// for sort. url.Values.Encode escapes the spaces in the group name for us.
	q.Set("sysparm_query", "assignment_group.name="+group+"^active=true^ORDERBYDESCsys_updated_on")
	q.Set("sysparm_limit", strconv.Itoa(limit))
	q.Set("sysparm_display_value", "true")
	q.Set("sysparm_fields", "number,short_description,state,priority,sys_updated_on,sys_id")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("servicenow: %w", err)
	}
	req.SetBasicAuth(c.User, c.Password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("servicenow: GET incidents: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("servicenow: %s returned %d: %s (check KA_SERVICENOW_URL and credentials)",
			u.Path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out struct {
		Result []struct {
			Number           string `json:"number"`
			ShortDescription string `json:"short_description"`
			State            string `json:"state"`
			Priority         string `json:"priority"`
			SysUpdatedOn     string `json:"sys_updated_on"`
			SysID            string `json:"sys_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("servicenow: decode response: %w", err)
	}
	base := strings.TrimSuffix(c.BaseURL, "/")
	incs := make([]Incident, 0, len(out.Result))
	for _, r := range out.Result {
		incs = append(incs, Incident{
			Number:           r.Number,
			ShortDescription: r.ShortDescription,
			State:            r.State,
			Priority:         r.Priority,
			UpdatedOn:        r.SysUpdatedOn,
			URL:              base + "/incident.do?sys_id=" + r.SysID,
		})
	}
	return incs, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/servicenow/ && go vet ./internal/servicenow/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/servicenow/
git commit -m "Add read-only ServiceNow Table API client"
```

---

### Task 2: config-driven per-team workgroup + contact

The workgroup (ServiceNow `assignment_group`) and contact must be changeable
without a rebuild, so they are **not** literals in `DefaultInfos()`. `Info` gains
the two fields; a `Metadata` type plus `ParseMetadata`/`ApplyMetadata` overlay
them from a JSON env var at startup (wired in Task 7).

**Files:**
- Modify: `internal/team/team.go` (the `Info` struct; add `Metadata`, `ParseMetadata`, `ApplyMetadata`)
- Test: `internal/team/metadata_test.go`

**Interfaces:**
- Produces:
  - `team.Info` gains `ServiceNowGroup string` and `Contact string` (unset in `DefaultInfos()`).
  - `type Metadata struct { ServiceNowGroup string "json:\"serviceNowGroup\""; Contact string "json:\"contact\"" }`
  - `func ParseMetadata(raw string) (map[string]Metadata, error)` — `""` → empty map, nil error; malformed JSON → error.
  - `func ApplyMetadata(infos []Info, meta map[string]Metadata) []Info` — returns a copy with matching slugs' fields filled; unknown slugs in `meta` ignored.

- [ ] **Step 1: Write the failing test**

Create `internal/team/metadata_test.go`:

```go
package team

import "testing"

func TestParseMetadata(t *testing.T) {
	m, err := ParseMetadata(`{"star":{"serviceNowGroup":"Star Support","contact":"star@x.com"}}`)
	if err != nil {
		t.Fatalf("ParseMetadata: %v", err)
	}
	if m["star"].ServiceNowGroup != "Star Support" || m["star"].Contact != "star@x.com" {
		t.Errorf("parsed star = %+v", m["star"])
	}
	empty, err := ParseMetadata("")
	if err != nil || len(empty) != 0 {
		t.Errorf("ParseMetadata(\"\") = %v, %v; want empty map, nil", empty, err)
	}
	if _, err := ParseMetadata("{not json"); err == nil {
		t.Error("ParseMetadata on malformed JSON = nil error, want error")
	}
}

func TestApplyMetadata(t *testing.T) {
	infos := []Info{{Slug: "star", DisplayName: "Star"}, {Slug: "hr", DisplayName: "HR"}}
	out := ApplyMetadata(infos, map[string]Metadata{
		"star":    {ServiceNowGroup: "Star Support", Contact: "s@x.com"},
		"unknown": {ServiceNowGroup: "Nope"},
	})
	bySlug := map[string]Info{}
	for _, i := range out {
		bySlug[i.Slug] = i
	}
	if bySlug["star"].ServiceNowGroup != "Star Support" || bySlug["star"].Contact != "s@x.com" {
		t.Errorf("star not overlaid: %+v", bySlug["star"])
	}
	if bySlug["hr"].ServiceNowGroup != "" {
		t.Errorf("hr should have no group, got %q", bySlug["hr"].ServiceNowGroup)
	}
	if len(out) != 2 {
		t.Errorf("unknown slug leaked a team: got %d infos, want 2", len(out))
	}
	// Input must not be mutated.
	if infos[0].ServiceNowGroup != "" {
		t.Error("ApplyMetadata mutated its input slice")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/team/ -run 'Metadata'`
Expected: FAIL — `Metadata`/`ParseMetadata`/`ApplyMetadata` undefined (compile error).

- [ ] **Step 3: Write minimal implementation**

In `internal/team/team.go`, extend `Info` (keep existing fields/comment), leaving `DefaultInfos()` unchanged (still just slug + display name):

```go
type Info struct {
	Slug        string
	DisplayName string
	SiteID      string // SharePoint site; unused until the ingestion plan
	// ServiceNowGroup is the incident assignment_group ("workgroup") queried
	// for this team's open tickets. Empty disables ticket lookup for the team.
	// Set from config (KA_TEAM_METADATA), not a literal, so a workgroup can
	// change without a rebuild.
	ServiceNowGroup string
	// Contact is the owning/on-call contact. Config-driven; stored for display,
	// not yet surfaced in answers.
	Contact string
}
```

Add to `internal/team/team.go` (new `Metadata` type and helpers; add `encoding/json` and `strings` to the import block):

```go
// Metadata is the per-team ticket configuration overlaid onto Info at startup.
// It is config-driven (KA_TEAM_METADATA) so a workgroup or contact can change
// without rebuilding the binary.
type Metadata struct {
	ServiceNowGroup string `json:"serviceNowGroup"`
	Contact         string `json:"contact"`
}

// ParseMetadata reads a JSON object of team slug -> Metadata. An empty string is
// a valid "nothing configured" input (empty map, no error); malformed JSON is an
// error so a typo fails loudly at startup rather than silently disabling tickets.
func ParseMetadata(raw string) (map[string]Metadata, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]Metadata{}, nil
	}
	var m map[string]Metadata
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("team: parse KA_TEAM_METADATA: %w (want a JSON object of slug -> {serviceNowGroup, contact})", err)
	}
	return m, nil
}

// ApplyMetadata returns a copy of infos with each team's ServiceNowGroup and
// Contact filled from meta by slug. Slugs in meta with no matching team are
// ignored; the input slice is not mutated.
func ApplyMetadata(infos []Info, meta map[string]Metadata) []Info {
	out := make([]Info, len(infos))
	copy(out, infos)
	for i := range out {
		if md, ok := meta[out[i].Slug]; ok {
			out[i].ServiceNowGroup = md.ServiceNowGroup
			out[i].Contact = md.Contact
		}
	}
	return out
}
```

`team.go` currently imports only `errors`; add `encoding/json`, `fmt`, and `strings`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/team/ && go vet ./internal/team/`
Expected: PASS (including the existing `TestDefaultInfosDisplayNames`, which still asserts only slug + display name).

- [ ] **Step 5: Commit**

```bash
git add internal/team/
git commit -m "Add config-driven per-team workgroup and contact metadata"
```

---

### Task 3: rag ticket types + heuristic gate

**Files:**
- Create: `internal/rag/ticket.go`
- Test: `internal/rag/ticket_test.go`

**Interfaces:**
- Consumes: `internal/team` (already imported by `rag`).
- Produces:
  - `type Ticket struct { Number, ShortDescription, State, Priority, UpdatedOn, URL string }`
  - `var ErrTeamNotConfigured = errors.New("rag: team has no ticket source configured")`
  - `type TicketSource interface { LatestOpen(ctx context.Context, t team.Team, limit int) ([]Ticket, error) }`
  - `type Classifier interface { Classify(ctx context.Context, question string) (bool, error) }`
  - `func isTicketQuery(msg string) bool`

- [ ] **Step 1: Write the failing test**

```go
package rag

import "testing"

func TestIsTicketQuery(t *testing.T) {
	yes := []string{
		"latest open ticket for Team star",
		"any open tickets?",
		"what's the status of INC0012345",
		"show me the incident",
		"TICKET please",
	}
	no := []string{
		"how does AVR send fields to Coupa?",
		"what is the submission deadline?",
		"hello there",
	}
	for _, q := range yes {
		if !isTicketQuery(q) {
			t.Errorf("isTicketQuery(%q) = false, want true", q)
		}
	}
	for _, q := range no {
		if isTicketQuery(q) {
			t.Errorf("isTicketQuery(%q) = true, want false", q)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rag/ -run TestIsTicketQuery`
Expected: FAIL — `isTicketQuery` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package rag

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/example/knowledge-assistant/internal/team"
)

// Ticket is one open ServiceNow incident, as summarized for the user.
type Ticket struct {
	Number           string
	ShortDescription string
	State            string
	Priority         string
	UpdatedOn        string
	URL              string
}

// ErrTeamNotConfigured means the caller's team has no ServiceNow group, so a
// ticket lookup is impossible — distinct from a transport failure.
var ErrTeamNotConfigured = errors.New("rag: team has no ticket source configured")

// TicketSource fetches a team's open tickets. The team comes from the request
// scope, never the message text.
type TicketSource interface {
	LatestOpen(ctx context.Context, t team.Team, limit int) ([]Ticket, error)
}

// Classifier reports whether a message is a ticket query. Optional fallback for
// phrasing the heuristic misses.
type Classifier interface {
	Classify(ctx context.Context, question string) (bool, error)
}

var incidentNumber = regexp.MustCompile(`(?i)\bINC\d+\b`)

// isTicketQuery is the heuristic fast-path of the intent gate: an INC number or
// the words "ticket"/"incident". Deterministic and cheap; the optional
// Classifier catches the rest.
func isTicketQuery(msg string) bool {
	if incidentNumber.MatchString(msg) {
		return true
	}
	low := strings.ToLower(msg)
	return strings.Contains(low, "ticket") || strings.Contains(low, "incident")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rag/ -run TestIsTicketQuery && go vet ./internal/rag/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rag/ticket.go internal/rag/ticket_test.go
git commit -m "Add ticket types and intent heuristic to rag"
```

---

### Task 4: ticket answer prompt

**Files:**
- Modify: `internal/rag/prompt.go` (append; reuse the `Prompt` type)
- Test: `internal/rag/prompt_test.go` (create if absent, else append)

**Interfaces:**
- Consumes: `Prompt`, `Turn`, `Ticket`.
- Produces: `func BuildTicketPrompt(question string, history []Turn, tickets []Ticket) Prompt`

- [ ] **Step 1: Write the failing test**

```go
package rag

import (
	"strings"
	"testing"
)

func TestBuildTicketPromptIncludesTicketData(t *testing.T) {
	p := BuildTicketPrompt("latest open ticket for star", nil, []Ticket{{
		Number: "INC0012345", ShortDescription: "VPN down", State: "In Progress",
		Priority: "2 - High", UpdatedOn: "2026-10-01 14:05:00", URL: "https://sn/incident.do?sys_id=abc",
	}})
	if !strings.Contains(p.User, "INC0012345") || !strings.Contains(p.User, "VPN down") {
		t.Errorf("ticket prompt user text missing ticket data:\n%s", p.User)
	}
	if !strings.Contains(strings.ToLower(p.System), "ticket") {
		t.Errorf("ticket system prompt does not mention tickets:\n%s", p.System)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rag/ -run TestBuildTicketPrompt`
Expected: FAIL — `BuildTicketPrompt` undefined.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/rag/prompt.go`:

```go
const ticketSystemPrompt = `You are a knowledge assistant. The user asked about their team's open support tickets. The live open tickets are given below as data — this is the only source; no documents were searched.

Write a short summary:
- Lead with one sentence (how many open tickets, and the most pressing one).
- Then one markdown bullet per ticket: its number in backticks, the short description, state, and priority.
- Reproduce ticket numbers, states and priorities exactly. Never invent a ticket, field or detail. Do not add tickets that are not listed.
- If a ticket has a URL, you may reference its number as the link text.`

// BuildTicketPrompt returns the model input for summarizing fetched tickets.
// There is no <context> document block — tickets are the grounding.
func BuildTicketPrompt(question string, history []Turn, tickets []Ticket) Prompt {
	var b strings.Builder
	b.WriteString("<tickets>\n")
	for i, t := range tickets {
		fmt.Fprintf(&b, "[%d] number=%s state=%q priority=%q updated=%q url=%s\n%s\n---\n",
			i+1, t.Number, t.State, t.Priority, t.UpdatedOn, t.URL, t.ShortDescription)
	}
	b.WriteString("</tickets>\n\n")
	fmt.Fprintf(&b, "Question: %s", question)
	return Prompt{System: ticketSystemPrompt, History: history, User: b.String()}
}
```

(`fmt` and `strings` are already imported in `prompt.go`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rag/ -run TestBuildTicketPrompt`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rag/prompt.go internal/rag/prompt_test.go
git commit -m "Add ticket-summary prompt builder"
```

---

### Task 5: orchestrator ticket branch

**Files:**
- Modify: `internal/rag/orchestrator.go` (add fields; add branch in `Answer`; add helpers)
- Test: `internal/rag/orchestrator_test.go` (append)

**Interfaces:**
- Consumes: `TicketSource`, `Classifier`, `Ticket`, `isTicketQuery`, `BuildTicketPrompt`, `ErrTeamNotConfigured`, existing `LLM`, `Scope`.
- Produces: `Orchestrator` gains `Tickets TicketSource`, `Classifier Classifier`, `TicketLimit int`.

- [ ] **Step 1: Write the failing test**

Append to `internal/rag/orchestrator_test.go`. These use small stubs; check the file for existing stub names (e.g. a stub LLM / retriever) and reuse them — define the ones below only if absent.

```go
// --- stubs (define only if not already present in this file) ---

type stubTicketSource struct {
	tickets []Ticket
	err     error
	called  bool
}

func (s *stubTicketSource) LatestOpen(ctx context.Context, t team.Team, limit int) ([]Ticket, error) {
	s.called = true
	return s.tickets, s.err
}

type stubClassifier struct {
	yes bool
	err error
}

func (s stubClassifier) Classify(ctx context.Context, q string) (bool, error) { return s.yes, s.err }

// collect drains an event channel into the concatenated token text and the
// set of event types seen.
func collect(t *testing.T, run func(out chan<- StreamEvent) error) (text string, types map[string]bool) {
	t.Helper()
	out := make(chan StreamEvent, 64)
	errc := make(chan error, 1)
	go func() { errc <- run(out); close(out) }()
	types = map[string]bool{}
	var b strings.Builder
	for ev := range out {
		types[ev.Type] = true
		if ev.Type == "token" {
			if s, ok := ev.Data.(string); ok {
				b.WriteString(s)
			}
		}
	}
	if err := <-errc; err != nil {
		t.Fatalf("Answer: %v", err)
	}
	return b.String(), types
}

func starScope(t *testing.T) Scope {
	t.Helper()
	reg := team.NewRegistry(team.DefaultInfos())
	star, err := reg.Parse("star")
	if err != nil {
		t.Fatal(err)
	}
	return NewScope(star, []team.Team{star}, nil)
}

// --- tests ---

func TestTicketBranchSummarizesFetchedTickets(t *testing.T) {
	src := &stubTicketSource{tickets: []Ticket{{Number: "INC0012345", ShortDescription: "VPN down", State: "In Progress"}}}
	o := &Orchestrator{LLM: echoPromptLLM{}, Tickets: src} // echoPromptLLM: see note
	text, types := collect(t, func(out chan<- StreamEvent) error {
		return o.Answer(context.Background(), "latest open ticket for star", nil, starScope(t), nil, out)
	})
	if !src.called {
		t.Error("ticket source was not called")
	}
	if !types["done"] {
		t.Error("no done event")
	}
	if !strings.Contains(text, "INC0012345") {
		t.Errorf("summary did not include ticket data; got %q", text)
	}
}

func TestTicketBranchEmptyGivesHonestStatus(t *testing.T) {
	src := &stubTicketSource{tickets: nil}
	o := &Orchestrator{LLM: echoPromptLLM{}, Tickets: src}
	text, _ := collect(t, func(out chan<- StreamEvent) error {
		return o.Answer(context.Background(), "any open tickets?", nil, starScope(t), nil, out)
	})
	if !strings.Contains(strings.ToLower(text), "no open tickets") {
		t.Errorf("empty result status = %q, want a 'no open tickets' message", text)
	}
}

func TestTicketBranchTransportErrorGivesHonestStatus(t *testing.T) {
	src := &stubTicketSource{err: errors.New("boom")}
	o := &Orchestrator{LLM: echoPromptLLM{}, Tickets: src}
	text, _ := collect(t, func(out chan<- StreamEvent) error {
		return o.Answer(context.Background(), "show me the ticket", nil, starScope(t), nil, out)
	})
	if !strings.Contains(strings.ToLower(text), "can't reach servicenow") && !strings.Contains(strings.ToLower(text), "cant reach servicenow") {
		t.Errorf("error status = %q, want a 'can't reach ServiceNow' message", text)
	}
}

func TestTicketBranchNotConfiguredGivesHonestStatus(t *testing.T) {
	src := &stubTicketSource{err: ErrTeamNotConfigured}
	o := &Orchestrator{LLM: echoPromptLLM{}, Tickets: src}
	text, _ := collect(t, func(out chan<- StreamEvent) error {
		return o.Answer(context.Background(), "open tickets", nil, starScope(t), nil, out)
	})
	if !strings.Contains(strings.ToLower(text), "isn't configured") && !strings.Contains(strings.ToLower(text), "isnt configured") {
		t.Errorf("not-configured status = %q", text)
	}
}

func TestTicketPhrasingFallsThroughToRAGWhenDisabled(t *testing.T) {
	rt := &stubRetriever{} // reuse the file's existing retriever stub; it records Search calls
	o := &Orchestrator{LLM: echoPromptLLM{}, Retriever: rt, TopK: 4, RerankN: 4} // Tickets nil
	_, _ = collect(t, func(out chan<- StreamEvent) error {
		return o.Answer(context.Background(), "any open tickets?", nil, starScope(t), nil, out)
	})
	if !rt.searched {
		t.Error("with Tickets nil, a ticket-phrased query must fall through to retrieval")
	}
}

func TestClassifierErrorFallsThroughToRAG(t *testing.T) {
	rt := &stubRetriever{}
	o := &Orchestrator{LLM: echoPromptLLM{}, Retriever: rt, TopK: 4, RerankN: 4,
		Tickets: &stubTicketSource{}, Classifier: stubClassifier{err: errors.New("timeout")}}
	_, _ = collect(t, func(out chan<- StreamEvent) error {
		// "status please" matches no heuristic; classifier errors -> RAG.
		return o.Answer(context.Background(), "what is the status please", nil, starScope(t), nil, out)
	})
	if !rt.searched {
		t.Error("classifier error must fall through to retrieval, not the ticket branch")
	}
}
```

Notes for the implementer:
- `echoPromptLLM` is a stub whose `Stream` emits the prompt's `User` text as one `token` event then returns nil — define it if the file lacks an equivalent streaming stub. This lets the success test assert the ticket data reached the prompt.
- `stubRetriever` with a `searched bool` set in `Search`: reuse the existing retriever stub in the file; add the `searched` flag if missing. Its `Search` should return a couple of chunks so the RAG path completes.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rag/ -run 'Ticket|Classifier'`
Expected: FAIL — new `Orchestrator` fields undefined / no ticket branch.

- [ ] **Step 3: Write minimal implementation**

Add fields to the `Orchestrator` struct in `orchestrator.go`:

```go
	// Tickets, when set, turns a ticket-intent turn into a live ServiceNow
	// lookup instead of a document search. Nil disables the feature entirely:
	// ticket phrasing then falls through to normal retrieval.
	Tickets TicketSource
	// Classifier is the optional LLM fallback of the intent gate, used only
	// when the heuristic does not match. Nil means heuristic-only.
	Classifier Classifier
	// TicketLimit caps how many open tickets are fetched and summarized.
	// Zero means 5.
	TicketLimit int
```

Add the branch in `Answer`, immediately after the greeting-gate block (after the `isGreeting` `return nil`):

```go
	// Ticket intent routes to a live ServiceNow lookup instead of retrieval.
	// Gated on Tickets being configured so ticket phrasing is a normal question
	// when the feature is off. Decided on intent only; the team is the scope's,
	// never the message's.
	if o.Tickets != nil && o.ticketIntent(ctx, question) {
		return o.answerTickets(ctx, question, history, scope, out)
	}
```

Add helpers at the end of `orchestrator.go`:

```go
func (o *Orchestrator) ticketLimit() int {
	if o.TicketLimit > 0 {
		return o.TicketLimit
	}
	return 5
}

// ticketIntent is the intent gate: heuristic first, then the optional
// classifier. A classifier error is not fatal — it falls back to "not a ticket
// query", so the turn becomes a normal document search rather than failing.
func (o *Orchestrator) ticketIntent(ctx context.Context, question string) bool {
	if isTicketQuery(question) {
		return true
	}
	if o.Classifier == nil {
		return false
	}
	yes, err := o.Classifier.Classify(ctx, question)
	if err != nil {
		o.logger().Warn("ticket classify failed; treating as a normal question", "err", err)
		return false
	}
	return yes
}

// answerTickets fetches the team's open tickets and streams a short summary.
// It never falls back to retrieval: the point of the feature is real-time
// truth, and answering a ticket question from documents would be a fabrication.
func (o *Orchestrator) answerTickets(ctx context.Context, question string, history []Turn, scope Scope, out chan<- StreamEvent) error {
	// Mirror the greeting gate: a ticket turn retrieves nothing, so the context
	// panel is explicitly empty rather than stale.
	out <- StreamEvent{Type: "retrieval", Data: []RetrievedChunk{}}
	out <- StreamEvent{Type: "citation", Data: []Chunk{}}

	tickets, err := o.Tickets.LatestOpen(ctx, scope.Team(), o.ticketLimit())
	switch {
	case errors.Is(err, ErrTeamNotConfigured):
		o.streamStatus(out, "Ticket lookup isn't configured for "+scope.Team().Slug()+".")
	case err != nil:
		o.logger().Warn("servicenow lookup failed", "err", err, "team", scope.Team().Slug())
		o.streamStatus(out, "Can't reach ServiceNow right now — try again in a moment.")
	case len(tickets) == 0:
		o.streamStatus(out, "No open tickets for "+scope.Team().Slug()+".")
	default:
		prompt := BuildTicketPrompt(question, history, tickets)
		o.tracePrompt(prompt)
		if err := o.LLM.Stream(ctx, prompt, out); err != nil {
			return fmt.Errorf("llm: %w", err)
		}
	}
	out <- StreamEvent{Type: "done"}
	return nil
}

// streamStatus sends a plain status line as a single token, so it is persisted
// and rendered exactly like a normal answer.
func (o *Orchestrator) streamStatus(out chan<- StreamEvent, msg string) {
	out <- StreamEvent{Type: "token", Data: msg}
}
```

Add `"errors"` to the `orchestrator.go` import block.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rag/ && go vet ./internal/rag/`
Expected: PASS (new and existing).

- [ ] **Step 5: Commit**

```bash
git add internal/rag/orchestrator.go internal/rag/orchestrator_test.go
git commit -m "Route ticket-intent turns to a live ServiceNow lookup"
```

---

### Task 6: chat-api ticketSource adapter

**Files:**
- Create: `services/chat-api/cmd/server/tickets.go`
- Test: `services/chat-api/cmd/server/tickets_test.go`

**Interfaces:**
- Consumes: `rag.TicketSource`, `rag.Ticket`, `rag.ErrTeamNotConfigured`, `team.Registry`, `servicenow.Client`, `servicenow.Incident`.
- Produces: `type ticketSource struct { reg *team.Registry; cli interface{ LatestOpen(context.Context, string, int) ([]servicenow.Incident, error) } }` implementing `rag.TicketSource`.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"context"
	"errors"
	"testing"

	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/servicenow"
	"github.com/example/knowledge-assistant/internal/team"
)

type stubSNClient struct {
	gotGroup string
	incs     []servicenow.Incident
}

func (s *stubSNClient) LatestOpen(ctx context.Context, group string, limit int) ([]servicenow.Incident, error) {
	s.gotGroup = group
	return s.incs, nil
}

func starTeam(t *testing.T) (team.Team, *team.Registry) {
	t.Helper()
	// Groups are config-driven, so overlay them the way main.go does.
	infos := team.ApplyMetadata(team.DefaultInfos(), map[string]team.Metadata{
		"star": {ServiceNowGroup: "Star Support", Contact: "star@x.com"},
	})
	reg := team.NewRegistry(infos)
	tm, err := reg.Parse("star")
	if err != nil {
		t.Fatal(err)
	}
	return tm, reg
}

func TestTicketSourceResolvesGroupAndMaps(t *testing.T) {
	tm, reg := starTeam(t)
	cli := &stubSNClient{incs: []servicenow.Incident{{Number: "INC1", ShortDescription: "x", URL: "u"}}}
	src := ticketSource{reg: reg, cli: cli}
	got, err := src.LatestOpen(context.Background(), tm, 5)
	if err != nil {
		t.Fatalf("LatestOpen: %v", err)
	}
	if cli.gotGroup != "Star Support" {
		t.Errorf("queried group = %q, want the star team's ServiceNowGroup", cli.gotGroup)
	}
	if len(got) != 1 || got[0].Number != "INC1" {
		t.Errorf("mapped tickets = %+v", got)
	}
}

func TestTicketSourceEmptyGroupIsNotConfigured(t *testing.T) {
	reg := team.NewRegistry([]team.Info{{Slug: "star", DisplayName: "Star"}}) // no ServiceNowGroup
	tm, _ := reg.Parse("star")
	src := ticketSource{reg: reg, cli: &stubSNClient{}}
	_, err := src.LatestOpen(context.Background(), tm, 5)
	if !errors.Is(err, rag.ErrTeamNotConfigured) {
		t.Errorf("err = %v, want ErrTeamNotConfigured", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./services/chat-api/cmd/server/ -run TestTicketSource`
Expected: FAIL — `ticketSource` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package main

import (
	"context"

	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/servicenow"
	"github.com/example/knowledge-assistant/internal/team"
)

// snClient is the slice of *servicenow.Client the adapter needs, named so tests
// can stub it.
type snClient interface {
	LatestOpen(ctx context.Context, group string, limit int) ([]servicenow.Incident, error)
}

// ticketSource adapts a ServiceNow client to rag.TicketSource: it resolves the
// team's assignment group from the registry and maps incidents to rag.Ticket.
// This is the only place servicenow and team config meet rag.
type ticketSource struct {
	reg *team.Registry
	cli snClient
}

func (s ticketSource) LatestOpen(ctx context.Context, t team.Team, limit int) ([]rag.Ticket, error) {
	group := s.reg.Info(t).ServiceNowGroup
	if group == "" {
		return nil, rag.ErrTeamNotConfigured
	}
	incs, err := s.cli.LatestOpen(ctx, group, limit)
	if err != nil {
		return nil, err
	}
	out := make([]rag.Ticket, len(incs))
	for i, in := range incs {
		out[i] = rag.Ticket{
			Number:           in.Number,
			ShortDescription: in.ShortDescription,
			State:            in.State,
			Priority:         in.Priority,
			UpdatedOn:        in.UpdatedOn,
			URL:              in.URL,
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./services/chat-api/cmd/server/ -run TestTicketSource`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add services/chat-api/cmd/server/tickets.go services/chat-api/cmd/server/tickets_test.go
git commit -m "Add chat-api adapter from ServiceNow client to rag.TicketSource"
```

---

### Task 7: config + wiring (feature on/off)

**Files:**
- Modify: `services/chat-api/internal/config/config.go`
- Modify: `services/chat-api/cmd/server/main.go`
- Modify: `.env.example`
- Test: `services/chat-api/internal/config/config_test.go` (append, if a config test file exists; otherwise add one)

**Interfaces:**
- Consumes: Task 1 `servicenow.Client`, Task 2 `team.ParseMetadata`/`ApplyMetadata`, Task 6 `ticketSource`, existing `secrets.FetchAPIKey`, `secrets.Client`.
- Produces: `Config` fields `ServiceNowMode, ServiceNowURL, ServiceNowUser, ServiceNowSecretID, ServiceNowPassword, TeamMetadata string`, `ServiceNowLimit int`, `ServiceNowTimeout time.Duration`; `orch.Tickets` wired when `KA_SERVICENOW_MODE=on`, using a registry built from `ApplyMetadata(DefaultInfos(), ParseMetadata(cfg.TeamMetadata))`.

- [ ] **Step 1: Write the failing test**

Append to the config test (match the existing table-test style in the package; set env, call the loader, assert). If no config test exists, create `config_test.go`:

```go
package config

import (
	"testing"
)

func TestLoadReadsServiceNow(t *testing.T) {
	t.Setenv("KA_SERVICENOW_MODE", "on")
	t.Setenv("KA_SERVICENOW_URL", "https://dev12345.service-now.com")
	t.Setenv("KA_SERVICENOW_USER", "svc")
	t.Setenv("KA_SERVICENOW_PASSWORD", "pw")
	t.Setenv("KA_TEAM_METADATA", `{"star":{"serviceNowGroup":"Star Support"}}`)
	c := Load() // use the package's actual loader name/signature
	if c.ServiceNowMode != "on" || c.ServiceNowURL == "" || c.ServiceNowUser != "svc" {
		t.Errorf("ServiceNow config not loaded: %+v", c)
	}
	if c.ServiceNowLimit != 5 {
		t.Errorf("ServiceNowLimit default = %d, want 5", c.ServiceNowLimit)
	}
	if c.TeamMetadata == "" {
		t.Error("TeamMetadata not loaded from KA_TEAM_METADATA")
	}
}
```

Adjust `Load()` to the real constructor name (check `config.go` — Task 0 reading).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./services/chat-api/internal/config/ -run ServiceNow`
Expected: FAIL — fields undefined.

- [ ] **Step 3: Write minimal implementation**

In `config.go`, add to the `Config` struct and the loader (follow the existing `envOr`/`envInt`/`envDuration` pattern):

```go
	ServiceNowMode     string
	ServiceNowURL      string
	ServiceNowUser     string
	ServiceNowSecretID string
	ServiceNowPassword string
	ServiceNowLimit    int
	ServiceNowTimeout  time.Duration
	TeamMetadata       string
```

In the loader literal:

```go
		ServiceNowMode:     envOr("KA_SERVICENOW_MODE", "off"),
		ServiceNowURL:      os.Getenv("KA_SERVICENOW_URL"),
		ServiceNowUser:     os.Getenv("KA_SERVICENOW_USER"),
		ServiceNowSecretID: os.Getenv("KA_SERVICENOW_SECRET_ID"),
		ServiceNowPassword: os.Getenv("KA_SERVICENOW_PASSWORD"),
		ServiceNowLimit:    envInt("KA_SERVICENOW_LIMIT", 5),
		ServiceNowTimeout:  envDuration("KA_SERVICENOW_TIMEOUT", 15*time.Second),
		TeamMetadata:       os.Getenv("KA_TEAM_METADATA"),
```

In `main.go`, after the rewrite switch (around line 191) and before the router is built, add:

```go
	switch cfg.ServiceNowMode {
	case "on":
		pw := cfg.ServiceNowPassword
		if cfg.ServiceNowSecretID != "" {
			sm, err := secrets.Client(context.Background())
			if err != nil {
				log.Error("servicenow secrets client", "err", err)
				os.Exit(1)
			}
			pw, err = secrets.FetchAPIKey(context.Background(), sm, cfg.ServiceNowSecretID)
			if err != nil {
				log.Error("servicenow password from secrets manager", "err", err)
				os.Exit(1)
			}
		}
		if cfg.ServiceNowURL == "" || cfg.ServiceNowUser == "" || pw == "" {
			log.Error("KA_SERVICENOW_MODE=on but URL, user, or password is missing")
			os.Exit(2)
		}
		meta, err := team.ParseMetadata(cfg.TeamMetadata)
		if err != nil {
			log.Error("team metadata", "err", err)
			os.Exit(2)
		}
		registry := team.NewRegistry(team.ApplyMetadata(team.DefaultInfos(), meta))
		orch.Tickets = ticketSource{
			reg: registry,
			cli: servicenow.Client{
				BaseURL:  cfg.ServiceNowURL,
				User:     cfg.ServiceNowUser,
				Password: pw,
				HTTP:     &http.Client{Timeout: cfg.ServiceNowTimeout},
			},
		}
		orch.TicketLimit = cfg.ServiceNowLimit
		log.Info("servicenow ticket lookup enabled", "url", cfg.ServiceNowURL, "user", cfg.ServiceNowUser)
	case "", "off":
		log.Info("servicenow ticket lookup disabled")
	default:
		log.Error("unknown KA_SERVICENOW_MODE; want \"on\" or \"off\"", "mode", cfg.ServiceNowMode)
		os.Exit(2)
	}
```

Add the `servicenow` import to `main.go`. Confirm `secrets` is already imported (the Anthropic path uses it); add it if not.

In `.env.example`, add (commented, off by default):

```bash
# ServiceNow live ticket lookup (local dev: URL, user and password here;
# in AWS the password comes from Secrets Manager via KA_SERVICENOW_SECRET_ID).
KA_SERVICENOW_MODE=off
# KA_SERVICENOW_URL=https://devXXXXX.service-now.com
# KA_SERVICENOW_USER=ka-integration
# KA_SERVICENOW_PASSWORD=
# KA_SERVICENOW_LIMIT=5
# Per-team workgroup + contact (JSON, slug -> {serviceNowGroup, contact}).
# Set serviceNowGroup to the exact assignment_group name in the instance.
# KA_TEAM_METADATA={"coupa":{"serviceNowGroup":"Coupa Middleware","contact":"coupa-oncall@example.com"},"star":{"serviceNowGroup":"Star Support","contact":"star-oncall@example.com"},"hr":{"serviceNowGroup":"HR Operations","contact":"hr-help@example.com"}}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./services/chat-api/... && go vet ./services/chat-api/...`
Expected: PASS and clean build (the whole app wires up).

- [ ] **Step 5: Commit**

```bash
git add services/chat-api/internal/config/ services/chat-api/cmd/server/main.go .env.example
git commit -m "Wire ServiceNow ticket lookup behind KA_SERVICENOW_MODE"
```

---

### Task 8: optional LLM classifier

This task is independent of Task 7 and can be skipped for a heuristic-only deploy. It adds the LLM fallback the spec's intent gate calls for.

**Files:**
- Create: `internal/classify/ollama.go`, `internal/classify/bedrock.go`, `internal/classify/prompt.go`
- Test: `internal/classify/classify_test.go`
- Modify: `services/chat-api/internal/config/config.go` (+`ServiceNowClassify string`), `services/chat-api/cmd/server/main.go` (wire `orch.Classifier`)

**Interfaces:**
- Produces: `classify.Ollama{BaseURL, Model string, HTTP *http.Client}` and `classify.Bedrock` (via `NewBedrock(model string, api converser)`), each with `Classify(ctx, question) (bool, error)` — satisfying `rag.Classifier`.

- [ ] **Step 1: Write the failing test**

```go
package classify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaClassifyParsesBool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"content": `{"ticket": true}`},
		})
	}))
	defer srv.Close()
	o := Ollama{BaseURL: srv.URL, Model: "gpt-oss:20b", HTTP: srv.Client()}
	yes, err := o.Classify(context.Background(), "any open tickets for star?")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !yes {
		t.Error("Classify = false, want true")
	}
}

func TestParseTicketBool(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{`{"ticket": true}`, true},
		{`sure: {"ticket": false} done`, false},
	} {
		got, err := parseTicketBool(tc.in)
		if err != nil {
			t.Fatalf("parseTicketBool(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("parseTicketBool(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if _, err := parseTicketBool("no json here"); err == nil {
		t.Error("parseTicketBool on junk = nil error, want error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/classify/`
Expected: FAIL — package undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/classify/prompt.go`:

```go
// Package classify decides whether a user message is a support-ticket query,
// the optional LLM fallback of the orchestrator's intent gate.
package classify

import (
	"fmt"
	"strings"
)

const systemPrompt = `Decide whether the user's message is asking about IT support tickets or incidents — for example open tickets, incident status, outages, or a specific INC number. Reply with JSON only: {"ticket": true} or {"ticket": false}. No other text.`

// parseTicketBool reads {"ticket": bool} out of a reply, tolerating prose around
// the JSON (as Bedrock's Converse may add).
func parseTicketBool(content string) (bool, error) {
	i, j := strings.IndexByte(content, '{'), strings.LastIndexByte(content, '}')
	if i < 0 || j < i {
		return false, fmt.Errorf("classify: no JSON object in %q", content)
	}
	var parsed struct {
		Ticket bool `json:"ticket"`
	}
	if err := jsonUnmarshal(content[i:j+1], &parsed); err != nil {
		return false, fmt.Errorf("classify: decode %q: %w", content[i:j+1], err)
	}
	return parsed.Ticket, nil
}
```

(Use a small `jsonUnmarshal` wrapper or inline `json.Unmarshal([]byte(...), ...)` — inline is fine; shown wrapped only to keep imports in one file. Prefer inlining `encoding/json` directly in `parseTicketBool` and dropping the wrapper.)

`internal/classify/ollama.go`:

```go
package classify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Ollama classifies with a chat model served by a local Ollama instance. It
// mirrors rewrite.Ollama's request shape.
type Ollama struct {
	BaseURL string
	Model   string
	HTTP    *http.Client
}

var boolSchema = map[string]any{
	"type":       "object",
	"properties": map[string]any{"ticket": map[string]any{"type": "boolean"}},
	"required":   []string{"ticket"},
}

func (o Ollama) client() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return http.DefaultClient
}

func (o Ollama) Classify(ctx context.Context, question string) (bool, error) {
	payload, err := json.Marshal(map[string]any{
		"model": o.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": question},
		},
		"stream":  false,
		"format":  boolSchema,
		"options": map[string]any{"temperature": 0},
	})
	if err != nil {
		return false, fmt.Errorf("classify: encode request: %w", err)
	}
	url := strings.TrimSuffix(o.BaseURL, "/") + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return false, fmt.Errorf("classify: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client().Do(req)
	if err != nil {
		return false, fmt.Errorf("classify: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return false, fmt.Errorf("classify: %s returned %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, fmt.Errorf("classify: decode response: %w", err)
	}
	return parseTicketBool(out.Message.Content)
}
```

`internal/classify/bedrock.go` (mirror `rewrite.Bedrock`):

```go
package classify

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

type converser interface {
	Converse(ctx context.Context, in *bedrockruntime.ConverseInput, opts ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

type Bedrock struct {
	Model string
	api   converser
}

func NewBedrock(model string, api converser) Bedrock { return Bedrock{Model: model, api: api} }

func (b Bedrock) Classify(ctx context.Context, question string) (bool, error) {
	out, err := b.api.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId: aws.String(b.Model),
		System:  []brtypes.SystemContentBlock{&brtypes.SystemContentBlockMemberText{Value: systemPrompt}},
		Messages: []brtypes.Message{{
			Role:    brtypes.ConversationRoleUser,
			Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: question}},
		}},
		InferenceConfig: &brtypes.InferenceConfiguration{Temperature: aws.Float32(0)},
	})
	if err != nil {
		return false, fmt.Errorf("classify: bedrock converse %s: %w", b.Model, err)
	}
	msg, ok := out.Output.(*brtypes.ConverseOutputMemberMessage)
	if !ok {
		return false, fmt.Errorf("classify: bedrock returned no message")
	}
	var text string
	for _, block := range msg.Value.Content {
		if t, ok := block.(*brtypes.ContentBlockMemberText); ok {
			text += t.Value
		}
	}
	return parseTicketBool(text)
}
```

Inline `encoding/json` in `parseTicketBool` (drop the `jsonUnmarshal` wrapper), so `prompt.go` imports `encoding/json`, `fmt`, `strings`.

- [ ] **Step 4: Wire it in config + main**

`config.go`: add `ServiceNowClassify string` and `ServiceNowClassify: envOr("KA_SERVICENOW_CLASSIFY", "off")`.

`main.go`, inside the `case "on":` of the ServiceNow switch (so the classifier only exists when the feature is on), after setting `orch.Tickets`:

```go
		switch cfg.ServiceNowClassify {
		case "ollama":
			orch.Classifier = classify.Ollama{BaseURL: cfg.RewriteURL, Model: cfg.RewriteModel, HTTP: &http.Client{Timeout: cfg.RewriteTimeout}}
			log.Info("ticket intent classifier enabled", "mode", "ollama", "model", cfg.RewriteModel)
		case "bedrock":
			bc, err := awsx.BedrockRuntime(context.Background())
			if err != nil {
				log.Error("bedrock classifier", "err", err)
				os.Exit(1)
			}
			orch.Classifier = classify.NewBedrock(cfg.RewriteModel, bc)
			log.Info("ticket intent classifier enabled", "mode", "bedrock", "model", cfg.RewriteModel)
		case "", "off":
			log.Info("ticket intent classifier disabled; heuristic only")
		default:
			log.Error("unknown KA_SERVICENOW_CLASSIFY; want \"ollama\", \"bedrock\" or \"off\"", "mode", cfg.ServiceNowClassify)
			os.Exit(2)
		}
```

Add `classify` import to `main.go`. In `.env.example`, add `# KA_SERVICENOW_CLASSIFY=off`.

Note: the spec listed `KA_SERVICENOW_CLASSIFY` as on/off; this plan refines it to off/ollama/bedrock so the backend is explicit (reusing the rewrite model/endpoint config). Update the spec's config line to match when implementing.

- [ ] **Step 5: Run tests and commit**

Run: `go build ./... && go test ./internal/classify/ ./services/chat-api/... && go vet ./...`
Expected: PASS.

```bash
git add internal/classify/ services/chat-api/internal/config/ services/chat-api/cmd/server/main.go .env.example docs/superpowers/specs/2026-10-01-servicenow-tickets-design.md
git commit -m "Add optional LLM ticket-intent classifier"
```

---

## Verification (whole feature)

- [ ] `go build ./... && go test ./... && go vet ./...` all clean.
- [ ] `make lint`.
- [ ] Manual, against the dev instance: set `KA_SERVICENOW_MODE=on` + URL/user/password in `.env`, `/run-local`, then ask "latest open ticket for star" and confirm a summary of real tickets; ask with the feature off and confirm the same phrasing goes through normal RAG.

## Self-Review

- **Spec coverage:** client (T1), config-driven per-team workgroup+contact (T2), intent gate heuristic + types (T3), summary prompt (T4), orchestrator branch + honest statuses + no-RAG-fallback (T5), team→group adapter (T6), config split local/AWS + feature gate + metadata wiring (T7), optional LLM classifier (T8). Team-never-from-text invariant enforced by using `scope.Team()` in T5. All spec sections mapped.
- **Placeholder scan:** none — every step carries real code or an exact command.
- **Type consistency:** `Ticket`, `TicketSource`, `Classifier`, `ErrTeamNotConfigured`, `isTicketQuery`, `BuildTicketPrompt`, `ticketSource`, `servicenow.Client.LatestOpen`, `Classify`, `team.Metadata`/`ParseMetadata`/`ApplyMetadata` names match across tasks. T6's `starTeam` helper overlays metadata the same way T7 wires it.
- **Review Focus:** all six lines have an owning task/test (T1 URL-encoding + non-2xx, T2/T7 malformed-metadata fail-fast, T5 error/empty/not-configured status + classifier-error fallthrough + disabled-feature fallthrough, T6 empty-group).
