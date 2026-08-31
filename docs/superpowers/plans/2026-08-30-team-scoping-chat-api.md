# Team Scoping (chat-api + UI) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Scope every retrieval, chat, and upload in chat-api to exactly one team resolved from the caller's identity, and surface the retrieved context in the UI.

**Architecture:** A validated `team.Team` type can only be produced by a registry parse. A `rag.Scope` — carrying the active team, the caller's other allowed teams, and ACL groups — is built once in middleware from the `X-Team` header intersected with the resolver's answer, then stored in the request context. The `rag.Retriever` interface takes a `Scope` instead of a `[]string` of groups, so every implementation must be updated and the compiler proves no retrieval path skips team scoping. OpenSearch moves from a `post_filter` to a filter inside the `knn` clause.

**Tech Stack:** Go 1.25, chi/v5, OpenSearch (lucene k-NN engine), Postgres, React 18 + Vite + Tailwind.

**Spec:** `docs/superpowers/specs/2026-08-30-team-segregation-design.md`

## Global Constraints

- Teams for this phase are exactly `coupa`, `star`, `hr`.
- Team is transported as the `X-Team` request header, not in the request body. This refines the spec, which said "request body": middleware cannot read a JSON body without consuming it, and putting the team in a header keeps resolution, authorisation, and `Scope` construction in one middleware with no handler involvement.
- Routes are under `/v1`, matching the existing router. The spec's `GET /api/teams` is `GET /v1/teams`.
- A team outside the caller's allowed set is a `403`, never a silent substitution.
- `team` and `aclGroups` remain separate index fields.
- The repository currently has **no test files**. Task 1 establishes the pattern: standard library `testing` only, no assertion libraries, table-driven where there is more than one case.
- Run `go build ./... && go test ./...` before every commit.

### On the strength of the "unforgeable" Scope

The spec calls `Scope` unforgeable. Go has no friend-package mechanism, so any package importing `rag` can call the constructor. The guarantee this plan actually delivers, and the one worth stating honestly:

1. A `Scope` cannot be built without a `team.Team`, and a `team.Team` cannot be built from a string without `Registry.Parse`, so a raw value from a request cannot become a scope by accident.
2. The `Retriever` interface requires a `Scope`, so the compiler proves no retrieval runs without one.
3. Authorisation lives in exactly one function, `team.Authorize`, with one call site in middleware — small enough to verify by reading, and pinned by the Task 5 test.

That is defence by construction plus a single audited chokepoint, not a language-level guarantee. Do not describe it as more than that.

---

### Task 1: The `team` package

**Files:**
- Create: `internal/team/team.go`
- Create: `internal/team/team_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `team.Team` (opaque, `Slug() string`, `IsZero() bool`); `team.Info{Slug, DisplayName, SiteID string}`; `team.Registry` with `NewRegistry(infos []Info) *Registry`, `(*Registry).Parse(slug string) (Team, error)`, `(*Registry).All() []Info`, `(*Registry).Info(t Team) Info`; `team.DefaultInfos() []Info`; `team.Authorize(allowed []Team, requested Team) error`; `team.ErrUnknownTeam`, `team.ErrNotAllowed`.

- [ ] **Step 1: Write the failing test**

Create `internal/team/team_test.go`:

```go
package team

import "testing"

func TestParseAcceptsOnlyRegisteredSlugs(t *testing.T) {
	r := NewRegistry(DefaultInfos())
	for _, slug := range []string{"coupa", "star", "hr"} {
		got, err := r.Parse(slug)
		if err != nil {
			t.Fatalf("Parse(%q) returned error %v, want nil", slug, err)
		}
		if got.Slug() != slug {
			t.Errorf("Parse(%q).Slug() = %q, want %q", slug, got.Slug(), slug)
		}
	}
	for _, slug := range []string{"", "COUPA", "finance", "coupa ", "../hr"} {
		if _, err := r.Parse(slug); err == nil {
			t.Errorf("Parse(%q) returned nil error, want ErrUnknownTeam", slug)
		}
	}
}

func TestZeroTeamIsInvalid(t *testing.T) {
	var zero Team
	if !zero.IsZero() {
		t.Fatal("zero Team reports IsZero() = false, want true")
	}
	if zero.Slug() != "" {
		t.Errorf("zero Team Slug() = %q, want empty", zero.Slug())
	}
}

func TestAuthorize(t *testing.T) {
	r := NewRegistry(DefaultInfos())
	coupa, _ := r.Parse("coupa")
	star, _ := r.Parse("star")
	hr, _ := r.Parse("hr")

	if err := Authorize([]Team{star, hr}, hr); err != nil {
		t.Errorf("Authorize(member of hr, hr) = %v, want nil", err)
	}
	if err := Authorize([]Team{star, hr}, coupa); err == nil {
		t.Error("Authorize(member of star+hr, coupa) = nil, want ErrNotAllowed")
	}
	if err := Authorize(nil, coupa); err == nil {
		t.Error("Authorize(no teams, coupa) = nil, want ErrNotAllowed")
	}
	var zero Team
	if err := Authorize([]Team{coupa}, zero); err == nil {
		t.Error("Authorize(coupa, zero team) = nil, want ErrNotAllowed")
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/team/...`
Expected: build failure — `undefined: NewRegistry`, `undefined: DefaultInfos`, `undefined: Team`.

- [ ] **Step 3: Write the implementation**

Create `internal/team/team.go`:

```go
// Package team defines the tenant boundary. A Team is a validated slug that
// can only be produced by a Registry, so a raw string from a request can
// never become a team by accident.
package team

import "errors"

var (
	ErrUnknownTeam = errors.New("team: unknown team")
	ErrNotAllowed  = errors.New("team: caller is not a member of this team")
)

// Team is an opaque, validated team identifier. The zero value is invalid.
type Team struct{ slug string }

func (t Team) Slug() string { return t.slug }
func (t Team) IsZero() bool { return t.slug == "" }

// Info is the configured description of one team.
type Info struct {
	Slug        string
	DisplayName string
	SiteID      string // SharePoint site; unused until the ingestion plan
}

// DefaultInfos is the team set for this phase.
func DefaultInfos() []Info {
	return []Info{
		{Slug: "coupa", DisplayName: "Coupa"},
		{Slug: "star", DisplayName: "Star"},
		{Slug: "hr", DisplayName: "HR"},
	}
}

type Registry struct {
	order  []Info
	bySlug map[string]Info
}

func NewRegistry(infos []Info) *Registry {
	r := &Registry{order: infos, bySlug: make(map[string]Info, len(infos))}
	for _, i := range infos {
		r.bySlug[i.Slug] = i
	}
	return r
}

// Parse turns an untrusted string into a Team, or fails.
func (r *Registry) Parse(slug string) (Team, error) {
	if _, ok := r.bySlug[slug]; !ok {
		return Team{}, ErrUnknownTeam
	}
	return Team{slug: slug}, nil
}

func (r *Registry) All() []Info { return r.order }

func (r *Registry) Info(t Team) Info { return r.bySlug[t.slug] }

// Authorize reports whether requested is in allowed. This is the only
// authorisation decision in the retrieval path; keep it that way.
func Authorize(allowed []Team, requested Team) error {
	if requested.IsZero() {
		return ErrNotAllowed
	}
	for _, a := range allowed {
		if a == requested {
			return nil
		}
	}
	return ErrNotAllowed
}
```

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/team/...`
Expected: `ok  github.com/example/knowledge-assistant/internal/team`

- [ ] **Step 5: Commit**

```bash
git add internal/team
git commit -m "Add team package with validated slugs and one authorization point"
```

---

### Task 2: `rag.Scope` and the Retriever interface change

This task deliberately breaks compilation across the repo and then fixes it. That breakage is the point: it enumerates every retrieval path.

**Files:**
- Create: `internal/rag/scope.go`
- Create: `internal/rag/scope_test.go`
- Modify: `internal/rag/types.go` (add `Team` to `Chunk`, change `Retriever`)
- Modify: `services/chat-api/internal/opensearch/memory.go`
- Modify: `services/chat-api/internal/opensearch/client.go`
- Modify: `internal/rag/orchestrator.go`
- Create: `services/chat-api/internal/opensearch/memory_test.go`

**Interfaces:**
- Consumes: `team.Team`, `team.Registry` from Task 1.
- Produces: `rag.Scope` with `NewScope(active team.Team, allowed []team.Team, groups []string) Scope`, `(Scope) Team() team.Team`, `(Scope) Others() []team.Team`, `(Scope) Groups() []string`, `(Scope) IsZero() bool`; `rag.Retriever.Search(ctx, query string, scope Scope, k int) ([]Chunk, error)`; `rag.Chunk.Team string`.

- [ ] **Step 1: Write the failing tests**

Create `internal/rag/scope_test.go`:

```go
package rag

import (
	"testing"

	"github.com/example/knowledge-assistant/internal/team"
)

func TestScopeCarriesActiveTeamAndOthers(t *testing.T) {
	r := team.NewRegistry(team.DefaultInfos())
	star, _ := r.Parse("star")
	hr, _ := r.Parse("hr")

	s := NewScope(star, []team.Team{star, hr}, []string{"everyone"})

	if s.Team() != star {
		t.Errorf("Team() = %q, want star", s.Team().Slug())
	}
	others := s.Others()
	if len(others) != 1 || others[0] != hr {
		t.Errorf("Others() = %v, want [hr]", others)
	}
	if s.IsZero() {
		t.Error("IsZero() = true for a constructed scope, want false")
	}
}

func TestZeroScopeIsRecognisable(t *testing.T) {
	var s Scope
	if !s.IsZero() {
		t.Fatal("zero Scope reports IsZero() = false; retrievers rely on this to refuse")
	}
}
```

Create `services/chat-api/internal/opensearch/memory_test.go`. The first test is the adversarial one from the spec — the nearest neighbours to the query are another team's chunks:

```go
package opensearch

import (
	"context"
	"testing"

	"strconv"

	"github.com/example/knowledge-assistant/internal/embed"
	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/team"
)

func scopeFor(t *testing.T, slug string, allowed ...string) rag.Scope {
	t.Helper()
	r := team.NewRegistry(team.DefaultInfos())
	active, err := r.Parse(slug)
	if err != nil {
		t.Fatalf("parse %q: %v", slug, err)
	}
	var as []team.Team
	for _, a := range allowed {
		p, err := r.Parse(a)
		if err != nil {
			t.Fatalf("parse %q: %v", a, err)
		}
		as = append(as, p)
	}
	return rag.NewScope(active, as, nil)
}

// seed inserts chunks whose text makes the HR chunks the nearest neighbours
// of the query, so a search that forgets to filter will surface them.
func seed(t *testing.T, m *MemoryStore) {
	t.Helper()
	ctx := context.Background()
	docs := []rag.Chunk{
		{ID: "hr-1", Team: "hr", PageTitle: "Escalation policy", Text: "escalation policy escalation policy"},
		{ID: "hr-2", Team: "hr", PageTitle: "Escalation matrix", Text: "escalation policy escalation matrix"},
		{ID: "coupa-1", Team: "coupa", PageTitle: "Invoice sync", Text: "invoice synchronisation with the supplier ledger"},
	}
	for _, d := range docs {
		if err := m.Upsert(ctx, d); err != nil {
			t.Fatalf("upsert %s: %v", d.ID, err)
		}
	}
}

func TestSearchNeverReturnsAnotherTeamsChunks(t *testing.T) {
	m := NewMemoryStore(embed.Mock{Dim: 1024})
	seed(t, m)

	got, err := m.Search(context.Background(), "escalation policy", scopeFor(t, "coupa", "coupa"), 8)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, c := range got {
		if c.Team != "coupa" {
			t.Fatalf("Search under coupa returned chunk %q from team %q", c.ID, c.Team)
		}
	}
}

// Spec test 3: the regression test for the post_filter starvation bug. The
// scoped team's corpus is larger than k, and another team's is larger still.
func TestSearchReturnsFullKWithinTheTeamPartition(t *testing.T) {
	m := NewMemoryStore(embed.Mock{Dim: 1024})
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		id := "hr-" + strconv.Itoa(i)
		if err := m.Upsert(ctx, rag.Chunk{ID: id, Team: "hr", Text: "escalation policy " + id}); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}
	for i := 0; i < 6; i++ {
		id := "coupa-" + strconv.Itoa(i)
		if err := m.Upsert(ctx, rag.Chunk{ID: id, Team: "coupa", Text: "escalation policy " + id}); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}

	got, err := m.Search(ctx, "escalation policy", scopeFor(t, "coupa", "coupa"), 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("Search returned %d chunks, want 5; the team filter must be applied before k, not after", len(got))
	}
}

func TestSearchRefusesZeroScope(t *testing.T) {
	m := NewMemoryStore(embed.Mock{Dim: 1024})
	seed(t, m)

	if _, err := m.Search(context.Background(), "escalation policy", rag.Scope{}, 8); err == nil {
		t.Fatal("Search with a zero Scope returned nil error, want refusal")
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/rag/... ./services/chat-api/internal/opensearch/...`
Expected: build failure — `undefined: NewScope`, `unknown field Team in struct literal of type rag.Chunk`.

- [ ] **Step 3: Create `internal/rag/scope.go`**

```go
package rag

import "github.com/example/knowledge-assistant/internal/team"

// Scope is the resolved retrieval scope for one request: which team's corpus
// is in play, which other teams the caller may switch to, and the caller's
// ACL groups.
//
// Its fields are unexported and it requires a team.Team, which only
// team.Registry.Parse can produce. A handler therefore cannot turn a string
// from a request into a Scope. Authorisation itself lives in team.Authorize,
// called once, in middleware.
type Scope struct {
	active  team.Team
	allowed []team.Team
	groups  []string
}

func NewScope(active team.Team, allowed []team.Team, groups []string) Scope {
	return Scope{active: active, allowed: allowed, groups: groups}
}

func (s Scope) Team() team.Team  { return s.active }
func (s Scope) Groups() []string { return s.groups }
func (s Scope) IsZero() bool     { return s.active.IsZero() }

// Others returns the caller's allowed teams excluding the active one. It backs
// the no-results escape hatch.
func (s Scope) Others() []team.Team {
	out := make([]team.Team, 0, len(s.allowed))
	for _, a := range s.allowed {
		if a != s.active {
			out = append(out, a)
		}
	}
	return out
}
```

- [ ] **Step 4: Update `internal/rag/types.go`**

Add `Team` to `Chunk` (immediately after `ID`) and change the `Retriever` signature:

```go
type Chunk struct {
	ID          string   `json:"id"`
	Team        string   `json:"team"`
	SpaceKey    string   `json:"spaceKey"`
	PageID      string   `json:"pageId"`
	PageTitle   string   `json:"pageTitle"`
	SectionPath string   `json:"sectionPath"`
	URL         string   `json:"url"`
	Text        string   `json:"text"`
	Score       float64  `json:"score"`
	ACLGroups   []string `json:"aclGroups"`
}

type Retriever interface {
	Search(ctx context.Context, query string, scope Scope, k int) ([]Chunk, error)
}
```

- [ ] **Step 5: Update `MemoryStore.Search` in `services/chat-api/internal/opensearch/memory.go`**

Replace the signature and the filter. Keep `aclOK` as it is and add the team check ahead of it:

```go
func (m *MemoryStore) Search(ctx context.Context, query string, scope rag.Scope, k int) ([]rag.Chunk, error) {
	if scope.IsZero() {
		return nil, errors.New("opensearch: search called with an unscoped request")
	}
	qv, err := m.Embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	type scored struct {
		c rag.Chunk
		s float64
	}
	results := make([]scored, 0, len(m.items))
	want := scope.Team().Slug()
	for _, it := range m.items {
		if it.chunk.Team != want {
			continue
		}
		if !aclOK(it.chunk.ACLGroups, scope.Groups()) {
			continue
		}
		results = append(results, scored{it.chunk, cosine(qv, it.vec)})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].s > results[j].s })
	if len(results) > k {
		results = results[:k]
	}
	out := make([]rag.Chunk, len(results))
	for i, r := range results {
		out[i] = r.c
		out[i].Score = r.s
	}
	return out, nil
}
```

Add `"errors"` to the imports.

- [ ] **Step 6: Update the remaining call sites so the package builds**

`services/chat-api/internal/opensearch/client.go`: change the signature to `Search(ctx context.Context, query string, scope rag.Scope, k int)`, add the same `scope.IsZero()` refusal at the top, and for now leave the query body alone except for replacing `aclFilter(groups)` with `aclFilter(scope.Groups())`. Task 3 rewrites the query properly.

`internal/rag/orchestrator.go`: change `Answer`'s `userGroups []string` parameter to `scope Scope` and pass `scope` through to both `session.Search` and `o.Retriever.Search`.

`services/chat-api/internal/handler/chat.go`: temporarily construct a scope so the build passes — Task 5 replaces this with the real context read. Delete `groupsFromCtx` and the `ctxGroupsKey` type, which are dead code (nothing has ever written that key, so ACL filtering has never actually run).

- [ ] **Step 7: Run the tests and confirm they pass**

Run: `go build ./... && go test ./...`
Expected: `ok` for `internal/team`, `internal/rag`, and `services/chat-api/internal/opensearch`.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "Thread a resolved Scope through the Retriever interface

Replaces the groups slice with a Scope carrying the active team, so the
compiler enumerates every retrieval path. Removes groupsFromCtx, which
read a context key nothing ever wrote."
```

---

### Task 3: Filtered k-NN in the OpenSearch client

**Files:**
- Modify: `services/chat-api/internal/opensearch/client.go`
- Create: `services/chat-api/internal/opensearch/client_test.go`
- Modify: `internal/index/ensure.go` (add the `team` field to the mapping)
- Modify: `internal/index/opensearch.go` (add `Team` to `index.Doc`)

**Interfaces:**
- Consumes: `rag.Scope` from Task 2.
- Produces: unchanged `Client.Search` signature; the index mapping gains `team` as a `keyword`; `index.Doc.Team string` with JSON tag `team`.

- [ ] **Step 1: Write the failing test**

The test asserts the shape of the request body, because that is where the isolation lives. Create `services/chat-api/internal/opensearch/client_test.go`:

```go
package opensearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/knowledge-assistant/internal/embed"
)

// captureBody stands in for OpenSearch and records the query it was sent.
func captureBody(t *testing.T, into *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, into); err != nil {
			t.Errorf("request body was not JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":{"hits":[]}}`))
	}))
}

func TestSearchFiltersInsideKNNNotAsPostFilter(t *testing.T) {
	var body map[string]any
	srv := captureBody(t, &body)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Index: "kb-chunks", HTTP: srv.Client(), Embedder: embed.Mock{Dim: 1024}}
	if _, err := c.Search(context.Background(), "invoice sync", scopeFor(t, "coupa", "coupa"), 8); err != nil {
		t.Fatalf("Search: %v", err)
	}

	if _, found := body["post_filter"]; found {
		t.Error("query still uses post_filter; k is applied before the team filter, which starves scoped results")
	}

	knn, ok := body["query"].(map[string]any)["knn"].(map[string]any)["embedding"].(map[string]any)
	if !ok {
		t.Fatalf("no knn.embedding clause in query: %#v", body)
	}
	filter, ok := knn["filter"]
	if !ok {
		t.Fatal("knn.embedding has no filter clause; the team filter must be inside the kNN")
	}
	if !containsTermTeam(filter, "coupa") {
		t.Errorf("kNN filter does not constrain team to coupa: %#v", filter)
	}
}

// containsTermTeam walks the filter looking for {"term":{"team":<slug>}}.
func containsTermTeam(v any, slug string) bool {
	switch n := v.(type) {
	case map[string]any:
		if term, ok := n["term"].(map[string]any); ok {
			if got, ok := term["team"].(string); ok && got == slug {
				return true
			}
		}
		for _, child := range n {
			if containsTermTeam(child, slug) {
				return true
			}
		}
	case []any:
		for _, child := range n {
			if containsTermTeam(child, slug) {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./services/chat-api/internal/opensearch/ -run TestSearchFilters -v`
Expected: FAIL — "query still uses post_filter" and "knn.embedding has no filter clause".

- [ ] **Step 3: Rewrite the query in `client.go`**

Replace the body construction and `aclFilter` with:

```go
	body := map[string]any{
		"size": k,
		"query": map[string]any{
			"knn": map[string]any{
				"embedding": map[string]any{
					"vector": vec,
					"k":      k,
					"filter": scopeFilter(scope),
				},
			},
		},
	}
```

and replace the `aclFilter` function with:

```go
// scopeFilter constrains a kNN search to the scope's team, and to chunks the
// caller's groups may see. It runs inside the knn clause so that k is applied
// within the team partition rather than across the whole index.
func scopeFilter(scope rag.Scope) map[string]any {
	must := []any{
		map[string]any{"term": map[string]any{"team": scope.Team().Slug()}},
	}
	if groups := scope.Groups(); len(groups) > 0 {
		// A chunk with no aclGroups is visible to every member of the team,
		// matching MemoryStore.aclOK.
		must = append(must, map[string]any{"bool": map[string]any{
			"minimum_should_match": 1,
			"should": []any{
				map[string]any{"terms": map[string]any{"aclGroups": groups}},
				map[string]any{"bool": map[string]any{
					"must_not": map[string]any{"exists": map[string]any{"field": "aclGroups"}},
				}},
			},
		}})
	}
	return map[string]any{"bool": map[string]any{"must": must}}
}
```

Delete the `TODO(hybrid)` comment's reference to the post-filter fallback and keep the rest of the doc comment.

- [ ] **Step 4: Add `team` to the index mapping**

In `internal/index/ensure.go`, add to `mappings.properties`, immediately after `"id"`:

```go
				"team":        map[string]any{"type": "keyword"},
```

In `internal/index/opensearch.go`, add to `Doc`, immediately after `ID`:

```go
	Team        string    `json:"team"`
```

- [ ] **Step 5: Refuse to index an unstamped chunk**

Spec test 5 asks that no chunk is indexed without a team. Enforce it in the
indexer rather than only testing the caller, so every future ingestion source
inherits the guarantee. Create `internal/index/bulk_test.go`:

```go
package index

import (
	"context"
	"strings"
	"testing"
)

func TestBulkRejectsDocsWithoutATeam(t *testing.T) {
	i := &Indexer{BaseURL: "http://unused", Index: "kb-chunks"}
	err := i.Bulk(context.Background(), []Doc{
		{ID: "c1", Team: "coupa", Text: "fine"},
		{ID: "c2", Text: "no team"},
	})
	if err == nil {
		t.Fatal("Bulk accepted a doc with no team; unstamped chunks are visible to every team's filter")
	}
	if !strings.Contains(err.Error(), "c2") {
		t.Errorf("error %q does not name the offending doc", err)
	}
}
```

Run: `go test ./internal/index/ -run TestBulkRejects -v` and confirm it fails.

Add the guard at the top of `Bulk` in `internal/index/opensearch.go`:

```go
	for _, d := range docs {
		if d.Team == "" {
			return fmt.Errorf("index: refusing to index doc %q with no team", d.ID)
		}
	}
```

Re-run and confirm it passes.

- [ ] **Step 6: Run the tests and confirm they pass**

Run: `go build ./... && go test ./...`
Expected: PASS, including `TestSearchFiltersInsideKNNNotAsPostFilter`.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "Filter by team inside the kNN clause instead of post-filtering

post_filter selected the global top-k and then discarded non-matching
hits, so a scoped query competing with a larger corpus returned a
near-empty context. Adds the team keyword to the index mapping."
```

---

### Task 4: Team resolution middleware

**Files:**
- Create: `services/chat-api/internal/middleware/scope.go`
- Create: `services/chat-api/internal/middleware/scope_test.go`
- Modify: `services/chat-api/internal/middleware/auth.go` (export the user ID onto the context)

**Interfaces:**
- Consumes: `team.Registry`, `team.Authorize`, `rag.NewScope`.
- Produces: `middleware.Resolver` interface with `AllowedTeams(ctx context.Context, userID string) ([]team.Team, error)`; `middleware.StaticResolver{Registry *team.Registry; Members map[string][]string}`; `middleware.WithScope(reg *team.Registry, res Resolver) func(http.Handler) http.Handler`; `middleware.ScopeFromContext(ctx) (rag.Scope, bool)`; `middleware.AllowedFromContext(ctx) ([]team.Team, bool)`; `middleware.UserFromContext(ctx) string`.

- [ ] **Step 1: Write the failing test**

Create `services/chat-api/internal/middleware/scope_test.go`:

```go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/knowledge-assistant/internal/team"
)

func testResolver() (*team.Registry, Resolver) {
	reg := team.NewRegistry(team.DefaultInfos())
	return reg, StaticResolver{
		Registry: reg,
		Members: map[string][]string{
			"a@example.com": {"coupa"},
			"b@example.com": {"star", "hr"},
		},
	}
}

// probe records the scope the middleware installed.
func probe(t *testing.T, userID, teamHeader string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	reg, res := testResolver()
	var sawTeam string
	h := WithScope(reg, res)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := ScopeFromContext(r.Context())
		if !ok {
			t.Error("handler reached with no scope in context")
			return
		}
		sawTeam = s.Team().Slug()
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/chats", nil)
	req.Header.Set("X-Dev-User", userID)
	if teamHeader != "" {
		req.Header.Set("X-Team", teamHeader)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, sawTeam
}

func TestScopeInstalledForAMemberTeam(t *testing.T) {
	rec, sawTeam := probe(t, "b@example.com", "hr")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if sawTeam != "hr" {
		t.Errorf("scope team = %q, want hr", sawTeam)
	}
}

func TestForgedTeamIsRejectedWithoutReachingTheHandler(t *testing.T) {
	rec, sawTeam := probe(t, "b@example.com", "coupa")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a team the caller is not in", rec.Code)
	}
	if sawTeam != "" {
		t.Errorf("handler ran with team %q; a forged team must not reach retrieval", sawTeam)
	}
}

func TestUnknownTeamIsRejected(t *testing.T) {
	rec, _ := probe(t, "b@example.com", "finance")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for an unregistered team", rec.Code)
	}
}

func TestMissingTeamHeaderIsRejectedNotDefaulted(t *testing.T) {
	rec, sawTeam := probe(t, "b@example.com", "")
	if rec.Code == http.StatusOK {
		t.Error("request with no X-Team succeeded; a missing team must not silently default")
	}
	if sawTeam != "" {
		t.Errorf("handler ran with team %q despite no X-Team header", sawTeam)
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./services/chat-api/internal/middleware/...`
Expected: build failure — `undefined: WithScope`, `undefined: StaticResolver`, `undefined: ScopeFromContext`.

- [ ] **Step 3: Write `services/chat-api/internal/middleware/scope.go`**

```go
package middleware

import (
	"context"
	"net/http"

	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/team"
)

type scopeCtxKey struct{}
type allowedCtxKey struct{}
type userCtxKey struct{}

// Resolver maps a user to the teams they belong to.
type Resolver interface {
	AllowedTeams(ctx context.Context, userID string) ([]team.Team, error)
}

// StaticResolver is the demo resolver: a hardcoded membership table. The
// production implementation reads Entra ID group claims from the verified JWT
// and maps group object IDs to teams; it satisfies this same interface, so
// swapping it touches only the wiring in cmd/server.
type StaticResolver struct {
	Registry *team.Registry
	Members  map[string][]string
}

func (s StaticResolver) AllowedTeams(_ context.Context, userID string) ([]team.Team, error) {
	var out []team.Team
	for _, slug := range s.Members[userID] {
		t, err := s.Registry.Parse(slug)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// DemoMembers is the membership table used in dev mode.
func DemoMembers() map[string][]string {
	return map[string][]string{
		"a@example.com":   {"coupa"},
		"b@example.com":   {"star", "hr"},
		"dev@example.com": {"coupa", "star", "hr"},
	}
}

// WithScope resolves the caller's teams, authorises the requested team from
// the X-Team header, and installs the resulting rag.Scope on the context.
// This is the only place team authorisation happens.
func WithScope(reg *team.Registry, res Resolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := userIDFrom(r)
			allowed, err := res.AllowedTeams(r.Context(), userID)
			if err != nil {
				http.Error(w, "resolve teams", http.StatusInternalServerError)
				return
			}

			ctx := context.WithValue(r.Context(), userCtxKey{}, userID)
			ctx = context.WithValue(ctx, allowedCtxKey{}, allowed)

			// The teams listing needs the allowed set but has no active team.
			if r.URL.Path == "/v1/teams" {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			requested, err := reg.Parse(r.Header.Get("X-Team"))
			if err == nil {
				err = team.Authorize(allowed, requested)
			}
			if err != nil {
				// Unknown and unauthorised are the same response: a caller
				// must not learn which teams exist by probing.
				http.Error(w, "team not available", http.StatusForbidden)
				return
			}

			scope := rag.NewScope(requested, allowed, GroupsFromContext(r.Context()))
			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, scopeCtxKey{}, scope)))
		})
	}
}

func ScopeFromContext(ctx context.Context) (rag.Scope, bool) {
	s, ok := ctx.Value(scopeCtxKey{}).(rag.Scope)
	return s, ok && !s.IsZero()
}

func AllowedFromContext(ctx context.Context) ([]team.Team, bool) {
	ts, ok := ctx.Value(allowedCtxKey{}).([]team.Team)
	return ts, ok
}

func UserFromContext(ctx context.Context) string {
	s, _ := ctx.Value(userCtxKey{}).(string)
	return s
}

func userIDFrom(r *http.Request) string {
	if v := r.Header.Get("X-Dev-User"); v != "" {
		return v
	}
	return "dev@example.com"
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./services/chat-api/internal/middleware/... -v`
Expected: all four tests PASS.

- [ ] **Step 5: Commit**

```bash
git add services/chat-api/internal/middleware
git commit -m "Resolve and authorize the caller's team in middleware

X-Team is intersected with the resolver's answer once, before any
handler runs. Unknown and unauthorized teams share a 403 so callers
cannot enumerate teams by probing."
```

---

### Task 5: Handlers read the scope; `GET /v1/teams`

**Files:**
- Modify: `services/chat-api/internal/handler/chat.go`
- Create: `services/chat-api/internal/handler/teams.go`
- Create: `services/chat-api/internal/handler/teams_test.go`
- Modify: `services/chat-api/cmd/server/main.go` (wire the middleware and the route)

**Interfaces:**
- Consumes: `middleware.ScopeFromContext`, `middleware.AllowedFromContext`, `team.Registry`.
- Produces: `handler.TeamsHandler{Registry *team.Registry}` serving `GET /v1/teams` as `[{"slug":"star","displayName":"Star"}, ...]`.

- [ ] **Step 1: Write the failing test**

Create `services/chat-api/internal/handler/teams_test.go`:

```go
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/knowledge-assistant/internal/team"
	"github.com/example/knowledge-assistant/services/chat-api/internal/middleware"
)

func TestTeamsListsOnlyTheCallersTeams(t *testing.T) {
	reg := team.NewRegistry(team.DefaultInfos())
	star, _ := reg.Parse("star")
	hr, _ := reg.Parse("hr")

	h := &TeamsHandler{Registry: reg}
	req := httptest.NewRequest(http.MethodGet, "/v1/teams", nil)
	req = req.WithContext(middleware.ContextWithAllowedForTest(context.Background(), []team.Team{star, hr}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %q)", err, rec.Body.String())
	}
	if len(got) != 2 {
		t.Fatalf("returned %d teams, want 2", len(got))
	}
	for _, g := range got {
		if g.Slug == "coupa" {
			t.Error("listing includes coupa, a team the caller does not belong to")
		}
		if g.DisplayName == "" {
			t.Errorf("team %q has no display name", g.Slug)
		}
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./services/chat-api/internal/handler/...`
Expected: build failure — `undefined: TeamsHandler`, `undefined: middleware.ContextWithAllowedForTest`.

- [ ] **Step 3: Add the test-only context helper**

In `services/chat-api/internal/middleware/scope.go`:

```go
// ContextWithAllowedForTest installs an allowed-team set without running the
// middleware. Test support only.
func ContextWithAllowedForTest(ctx context.Context, allowed []team.Team) context.Context {
	return context.WithValue(ctx, allowedCtxKey{}, allowed)
}
```

- [ ] **Step 4: Write `services/chat-api/internal/handler/teams.go`**

```go
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/example/knowledge-assistant/internal/team"
	"github.com/example/knowledge-assistant/services/chat-api/internal/middleware"
)

// TeamsHandler serves the teams the caller may select. The UI renders its
// dropdown from this, so the team list is never hardcoded in the frontend.
type TeamsHandler struct {
	Registry *team.Registry
}

type teamDTO struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
}

func (h *TeamsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	allowed, _ := middleware.AllowedFromContext(r.Context())
	out := make([]teamDTO, 0, len(allowed))
	for _, t := range allowed {
		info := h.Registry.Info(t)
		out = append(out, teamDTO{Slug: info.Slug, DisplayName: info.DisplayName})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
```

- [ ] **Step 5: Update `chat.go` to read the real scope**

Replace the temporary scope from Task 2 Step 6 with:

```go
	scope, ok := middleware.ScopeFromContext(r.Context())
	if !ok {
		http.Error(w, "no team scope", http.StatusForbidden)
		return
	}
	uid := middleware.UserFromContext(r.Context())
```

and pass `scope` to `h.Orchestrator.Answer`. Delete the now-unused `userID` helper in `chats.go`, replacing its call sites with `middleware.UserFromContext(r.Context())`.

- [ ] **Step 6: Wire it in `services/chat-api/cmd/server/main.go`**

Before the `authed := ...` line:

```go
	registry := team.NewRegistry(team.DefaultInfos())
	resolver := middleware.StaticResolver{Registry: registry, Members: middleware.DemoMembers()}
```

Change the middleware chain and add the route:

```go
	authed := r.With(middleware.Auth(true /* dev */), middleware.WithScope(registry, resolver))
	authed.Method(http.MethodGet, "/v1/teams", &handler.TeamsHandler{Registry: registry})
```

- [ ] **Step 7: Run the tests and confirm they pass**

Run: `go build ./... && go test ./...`
Expected: all packages `ok`.

- [ ] **Step 8: Verify by hand**

```bash
make run-api &
curl -s -H 'X-Dev-User: b@example.com' localhost:8080/v1/teams
# expect [{"slug":"star",...},{"slug":"hr",...}] — no coupa
curl -s -o /dev/null -w '%{http_code}\n' -X POST localhost:8080/v1/chat/messages \
  -H 'X-Dev-User: b@example.com' -H 'X-Team: coupa' \
  -H 'Content-Type: application/json' -d '{"message":"hello"}'
# expect 403
```

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "Serve the caller's teams and scope chat requests from context"
```

---

### Task 6: The `retrieval` event

**Files:**
- Modify: `internal/rag/orchestrator.go`
- Create: `internal/rag/orchestrator_test.go`
- Modify: `internal/rag/types.go` (add `RetrievedChunk`)

**Interfaces:**
- Consumes: `rag.Scope`, `rag.Chunk`.
- Produces: `rag.RetrievedChunk{Chunk; Used bool}`; a `StreamEvent` of type `"retrieval"` carrying `[]RetrievedChunk`, emitted before the `citation` event.

- [ ] **Step 1: Write the failing test**

Create `internal/rag/orchestrator_test.go`:

```go
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
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/rag/ -run TestRetrieval -v`
Expected: build failure — `undefined: RetrievedChunk`.

- [ ] **Step 3: Add `RetrievedChunk` to `internal/rag/types.go`**

```go
// RetrievedChunk is a chunk as shown in the UI's context panel. Used reports
// whether it was passed to the model; chunks that were retrieved and then cut
// are the most useful signal when an answer is wrong, so they are sent too.
type RetrievedChunk struct {
	Chunk
	Used bool `json:"used"`
}
```

Extend the `StreamEvent` doc comment to list `"retrieval"`:

```go
	Type string `json:"type"` // "retrieval" | "token" | "citation" | "done" | "error"
```

- [ ] **Step 4: Emit the event from `Answer`**

Track everything retrieved, then mark. Replace the tail of `Answer` (from the `out <- StreamEvent{Type: "citation", ...}` line) with:

```go
	out <- StreamEvent{Type: "retrieval", Data: mark(all, chosen)}
	out <- StreamEvent{Type: "citation", Data: chosen}
```

Accumulate `all` alongside `chosen`: in each of the two retrieval blocks, append every returned chunk to a `var all []Chunk` before the dedup-and-select loop runs, skipping IDs already in `all`. Add:

```go
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
```

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `go test ./internal/rag/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "Stream a retrieval event carrying every chunk and whether it was used

The chunks that were retrieved and then cut are what explains a wrong
answer, so they are sent alongside the ones that reached the prompt."
```

---

### Task 7: No-results escape hatch

**Files:**
- Modify: `internal/rag/types.go` (add `Counter`)
- Modify: `internal/rag/orchestrator.go`
- Modify: `internal/rag/orchestrator_test.go`
- Modify: `services/chat-api/internal/opensearch/memory.go` (implement `Count`)
- Modify: `services/chat-api/internal/config/config.go` (add `RelevanceFloor`)

**Interfaces:**
- Consumes: `rag.Scope.Others()`.
- Produces: `rag.Counter` with `Count(ctx context.Context, query string, scope Scope) (int, error)`; `Orchestrator.Counter Counter` and `Orchestrator.RelevanceFloor float64`; a `StreamEvent` of type `"suggestion"` carrying `[]rag.TeamSuggestion{Team string; DisplayName string; Matches int}`.

- [ ] **Step 1: Write the failing test**

Append to `internal/rag/orchestrator_test.go`:

```go
type stubCounter struct{ counts map[string]int }

func (s stubCounter) Count(_ context.Context, _ string, scope Scope) (int, error) {
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
	if strings.Contains(llm.prompt, "hr") {
		t.Error("another team's name leaked into the prompt")
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
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/rag/ -run TestSuggest -v`
Expected: build failure — `undefined: TeamSuggestion`, `unknown field Counter`.

- [ ] **Step 3: Add the types to `internal/rag/types.go`**

```go
// Counter reports how many chunks a query would match in a scope, without
// returning any of them. It backs the no-results escape hatch, which tells a
// multi-team user that their question is answerable under another of their
// teams while leaking nothing but a count.
type Counter interface {
	Count(ctx context.Context, query string, scope Scope) (int, error)
}

// TeamSuggestion is a count only. It must never carry chunk text or titles.
type TeamSuggestion struct {
	Team        string `json:"team"`
	DisplayName string `json:"displayName"`
	Matches     int    `json:"matches"`
}
```

- [ ] **Step 4: Implement the probe in `Answer`**

Add the fields to `Orchestrator`:

```go
	Counter        Counter
	RelevanceFloor float64
```

After `chosen` is assembled and before the retrieval event, insert:

```go
	// Nothing relevant here: tell the caller whether another of their teams
	// can answer, without retrieving anything from it.
	if o.Counter != nil && belowFloor(chosen, o.RelevanceFloor) {
		var suggestions []TeamSuggestion
		for _, other := range scope.Others() {
			probe := NewScope(other, nil, scope.Groups())
			n, err := o.Counter.Count(ctx, question, probe)
			if err != nil || n == 0 {
				continue
			}
			suggestions = append(suggestions, TeamSuggestion{Team: other.Slug(), Matches: n})
		}
		if len(suggestions) > 0 {
			out <- StreamEvent{Type: "suggestion", Data: suggestions}
		}
	}
```

and:

```go
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
```

- [ ] **Step 5: Implement `Count` on `MemoryStore`**

In `services/chat-api/internal/opensearch/memory.go`:

```go
// Count reports how many chunks in the scope's team the query would match,
// without returning any of them.
func (m *MemoryStore) Count(ctx context.Context, query string, scope rag.Scope) (int, error) {
	hits, err := m.Search(ctx, query, scope, 1000)
	if err != nil {
		return 0, err
	}
	return len(hits), nil
}
```

- [ ] **Step 6: Add the config knob**

In `services/chat-api/internal/config/config.go`, add `RelevanceFloor float64` to `Config`, and to `Load()`:

```go
		RelevanceFloor:  envFloat("KA_RELEVANCE_FLOOR", 0.25),
```

with:

```go
func envFloat(k string, d float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return d
}
```

Wire `RelevanceFloor` and `Counter` into the `Orchestrator` construction in `services/chat-api/cmd/server/main.go`, setting `Counter` to the same store used as `Retriever` when it implements `rag.Counter`:

```go
	orch := &rag.Orchestrator{
		Retriever: retriever, LLM: llm, TopK: cfg.TopK, RerankN: cfg.RerankTopN,
		RelevanceFloor: cfg.RelevanceFloor,
	}
	if c, ok := retriever.(rag.Counter); ok {
		orch.Counter = c
	}
```

- [ ] **Step 7: Run the tests and confirm they pass**

Run: `go build ./... && go test ./...`
Expected: all `ok`.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "Suggest the caller's other teams when a scoped search finds nothing

Counts only: no chunk text or titles cross the team boundary, and the
suggestion never enters the prompt."
```

---

### Task 8: Team-scoped chat persistence

**Files:**
- Modify: `services/chat-api/internal/repo/schema.sql`
- Modify: `services/chat-api/internal/repo/repo.go`
- Modify: `services/chat-api/internal/handler/chats.go`
- Modify: `services/chat-api/internal/handler/chat.go`

**Interfaces:**
- Consumes: `middleware.ScopeFromContext`.
- Produces: `Repo.CreateChat(ctx, userID, teamSlug, title string) (Chat, error)`; `Repo.ListChats(ctx, userID, teamSlug string) ([]Chat, error)`; `repo.Chat.Team string`.

- [ ] **Step 1: Add the column and index to `schema.sql`**

The column is added with a default so the statement is safe against an existing table:

```sql
ALTER TABLE chats ADD COLUMN IF NOT EXISTS team TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_chats_user_team_updated ON chats (user_id, team, updated_at DESC);
```

Add `team TEXT NOT NULL DEFAULT ''` to the `CREATE TABLE chats` body as well, so a fresh database and a migrated one agree.

- [ ] **Step 2: Update `repo.go`**

Add `Team string \`json:"team"\`` to the `Chat` struct, then change the two queries:

```go
func (r *Repo) CreateChat(ctx context.Context, userID, teamSlug, title string) (Chat, error) {
	var c Chat
	err := r.pool.QueryRow(ctx,
		`INSERT INTO chats (user_id, team, title)
		 VALUES ($1, $2, $3)
		 RETURNING id, user_id, team, title, created_at, updated_at`,
		userID, teamSlug, title,
	).Scan(&c.ID, &c.UserID, &c.Team, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func (r *Repo) ListChats(ctx context.Context, userID, teamSlug string) ([]Chat, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, team, title, created_at, updated_at
		 FROM chats
		 WHERE user_id = $1 AND team = $2
		 ORDER BY updated_at DESC`,
		userID, teamSlug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chats := []Chat{}
	for rows.Next() {
		var c Chat
		if err := rows.Scan(&c.ID, &c.UserID, &c.Team, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		chats = append(chats, c)
	}
	return chats, rows.Err()
}
```

Match the existing driver and receiver names in `repo.go` if they differ from `r.pool`; the query text and the added `team` column are what matter.

- [ ] **Step 3: Update the handlers**

In `chat.go`, pass `scope.Team().Slug()` to `CreateChat`. In `chats.go`, read the scope and pass the team to `ListChats`; in `Create`, do the same.

- [ ] **Step 4: Verify by hand**

There is no Postgres test harness in this repo, so verify against the dev database:

```bash
make dev-up
make run-api &
curl -s -X POST localhost:8080/v1/chats -H 'X-Dev-User: b@example.com' \
  -H 'X-Team: star' -H 'Content-Type: application/json' -d '{"title":"star chat"}'
curl -s localhost:8080/v1/chats -H 'X-Dev-User: b@example.com' -H 'X-Team: star'
# expect the star chat
curl -s localhost:8080/v1/chats -H 'X-Dev-User: b@example.com' -H 'X-Team: hr'
# expect [] — the star chat must not appear under hr
```

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "Scope persisted chats to the team they were created under"
```

---

### Task 9: Team-scoped uploads and fixtures

**Files:**
- Modify: `services/chat-api/internal/handler/upload.go`
- Modify: `services/chat-api/internal/session/store.go`
- Modify: `services/chat-api/cmd/server/main.go` (`preloadFixtures`)
- Create: `fixtures/coupa/`, `fixtures/star/`, `fixtures/hr/`
- Modify: `Makefile` (`seed` target)

**Interfaces:**
- Consumes: `rag.Scope`.
- Produces: `session.Store.Add(ctx, sessionID, teamSlug string, u Upload, chunks []rag.Chunk) error`; `session.Store.Retriever(sessionID, teamSlug string) rag.Retriever`.

- [ ] **Step 1: Write the failing test**

Create `services/chat-api/internal/session/store_test.go`:

```go
package session

import (
	"context"
	"testing"
	"time"

	"github.com/example/knowledge-assistant/internal/embed"
	"github.com/example/knowledge-assistant/internal/rag"
)

func TestUploadsAreInvisibleUnderAnotherTeam(t *testing.T) {
	s := NewStore(embed.Mock{Dim: 1024}, time.Minute)
	ctx := context.Background()
	chunks := []rag.Chunk{{ID: "u1", Team: "star", PageTitle: "Draft", Text: "star only upload"}}

	if err := s.Add(ctx, "sess-1", "star", Upload{ID: "u1", Filename: "draft.md"}, chunks); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if r := s.Retriever("sess-1", "star"); r == nil {
		t.Error("Retriever for the uploading team is nil, want the session store")
	}
	if r := s.Retriever("sess-1", "hr"); r != nil {
		t.Error("an upload made under star is retrievable under hr")
	}
}
```

- [ ] **Step 2: Key session entries by team**

Run `go test ./services/chat-api/internal/session/...` and confirm it fails to build (`too many arguments in call to s.Add`).

In `session/store.go`, add the key helper and thread the team through:

```go
// key scopes a session's uploads to the team they were made under, so an
// upload cannot surface in a different team's retrieval.
func key(sessionID, teamSlug string) string { return sessionID + "\x00" + teamSlug }
```

Change `Add(ctx, sessionID string, u Upload, chunks []rag.Chunk)` to `Add(ctx, sessionID, teamSlug string, u Upload, chunks []rag.Chunk)` and look up `s.sessions[key(sessionID, teamSlug)]`. Change `Retriever(sessionID string)` to `Retriever(sessionID, teamSlug string)` and use the same key. The `gc` loop is unaffected.

Re-run the test and confirm it passes.

- [ ] **Step 3: Stamp uploads with the team**

In `upload.go`, read the scope and replace `SpaceKey: "UPLOAD"` with `Team: scope.Team().Slug(), SpaceKey: "UPLOAD"` on the chunks, and pass the team through to `Sessions.Add`.

- [ ] **Step 4: Split the fixtures by team**

```bash
mkdir -p fixtures/coupa fixtures/star fixtures/hr
git mv fixtures/icertis-coupa-integration.md fixtures/coupa/
git mv fixtures/avr-integration.md fixtures/star/
git mv fixtures/salesforce-integration.md fixtures/star/
git mv fixtures/servicenow-integration.md fixtures/coupa/
cat > fixtures/hr/leave-policy.md <<'MD'
# Leave Policy

## Annual leave

Employees accrue 25 days of annual leave per calendar year, accrued monthly.

## Escalation

Disputes about leave balances are escalated to the HR business partner for
the employee's division, then to the Head of People.
MD
```

The HR fixture deliberately contains an "Escalation" section so that a Star-scoped question about escalation exercises the no-results escape hatch against real data.

- [ ] **Step 5: Preload fixtures per team**

Change `preloadFixtures` to walk one subdirectory per team, deriving the team from the directory name and setting `Team:` on each chunk. Chunk IDs become `team + ":" + pageID + ":" + index` so identically-named files in two teams cannot collide.

- [ ] **Step 6: Update the seed target**

```make
seed:
	go run ./services/ingestion/cmd/indexer -source fixtures -fixtures fixtures/coupa -team coupa
	go run ./services/ingestion/cmd/indexer -source fixtures -fixtures fixtures/star  -team star
	go run ./services/ingestion/cmd/indexer -source fixtures -fixtures fixtures/hr    -team hr
```

Add the `-team` flag to `services/ingestion/cmd/indexer/main.go`, parse it through `team.Registry.Parse`, exit non-zero if it is missing or unknown, and set `Team` on every `index.Doc`. Team is never inferred from the file path — the flag is the only source.

- [ ] **Step 7: Verify**

```bash
go build ./... && go test ./...
KA_FIXTURES_DIR=fixtures make run-api &
curl -s -X POST localhost:8080/v1/chat/messages \
  -H 'X-Dev-User: b@example.com' -H 'X-Team: star' \
  -H 'Content-Type: application/json' -d '{"message":"what is the escalation process"}'
# expect a suggestion event naming hr, and no HR text in any citation
```

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "Scope uploads and fixtures by team

Fixtures move into per-team directories and the indexer takes team as a
required flag rather than inferring it from content or path."
```

---

### Task 10: UI — team selector and context panel

**Files:**
- Modify: `ui/src/api.js`
- Modify: `ui/src/App.jsx`
- Modify: `ui/src/components/TopBar.jsx`
- Create: `ui/src/components/ContextPanel.jsx`

**Interfaces:**
- Consumes: `GET /v1/teams`; the `retrieval` and `suggestion` SSE events.
- Produces: no exported contract; this is the last task.

There is no JS test runner configured in `ui/`, so this task verifies by hand.

- [ ] **Step 1: Send the team on every request**

In `ui/src/api.js`, change `headers` to take the active team and add `listTeams`:

```js
const headers = (team) => ({ 'X-Dev-Groups': 'everyone', ...(team ? { 'X-Team': team } : {}) });

export async function listTeams() {
  const res = await fetch('/v1/teams', { headers: headers() });
  if (!res.ok) throw new Error('list teams failed');
  return res.json();
}
```

Add a `team` parameter to `listChats`, `getMessages`, `deleteChat`, `uploadFile`, and `streamChat`, passing it to `headers(team)`.

- [ ] **Step 2: Hold the active team in `App.jsx`**

Add `const [teams, setTeams] = useState([]);` and `const [team, setTeam] = useState(null);`. Load teams on mount and preselect the first:

```jsx
  useEffect(() => {
    listTeams().then(ts => { setTeams(ts); setTeam(ts[0]?.slug ?? null); }).catch(() => setTeams([]));
  }, []);
```

Gate the existing chat-loading effects on `team` being set, pass `team` into every api call, and call `newChat()` whenever `team` changes — a chat's team is fixed at creation, so switching teams starts a new conversation rather than re-scoping the current one.

- [ ] **Step 3: Render the selector in `TopBar.jsx`**

```jsx
export default function TopBar({ title, onNewChat, teams, team, onTeamChange }) {
  return (
    <header className="h-14 border-b border-slate-200 bg-white flex items-center px-4 gap-4">
      <div className="flex items-center gap-2">
        <div className="w-8 h-8 rounded-md bg-slate-900 text-white flex items-center justify-center font-semibold">
          KA
        </div>
        <span className="font-semibold text-slate-800">Knowledge Assistant</span>
      </div>
      <select
        value={team ?? ''}
        onChange={e => onTeamChange(e.target.value)}
        className="text-sm border border-slate-300 rounded-md px-2 py-1.5 bg-white">
        {teams.map(t => <option key={t.slug} value={t.slug}>{t.displayName}</option>)}
      </select>
      <div className="flex-1 text-sm text-slate-500 truncate">{title}</div>
      <button
        onClick={onNewChat}
        className="px-3 py-1.5 text-sm rounded-md border border-slate-300 hover:bg-slate-50">
        + New chat
      </button>
    </header>
  );
}
```

Disable the composer while `team` is null.

- [ ] **Step 4: Create `ui/src/components/ContextPanel.jsx`**

```jsx
export default function ContextPanel({ chunks, open, onToggle }) {
  if (!chunks?.length) return null;
  const used = chunks.filter(c => c.used);
  const dropped = chunks.filter(c => !c.used);

  return (
    <aside className={`${open ? 'w-96' : 'w-10'} border-l border-slate-200 bg-slate-50 flex flex-col transition-all`}>
      <button
        onClick={onToggle}
        className="h-10 shrink-0 text-xs text-slate-500 hover:text-slate-800 border-b border-slate-200">
        {open ? 'Hide context' : '‹'}
      </button>
      {open && (
        <div className="flex-1 overflow-y-auto p-3 space-y-3">
          <p className="text-xs font-medium text-slate-500 uppercase tracking-wide">
            Passed to the model ({used.length})
          </p>
          {used.map(c => <ChunkCard key={c.id} chunk={c} />)}
          {dropped.length > 0 && (
            <>
              <p className="text-xs font-medium text-slate-400 uppercase tracking-wide pt-2 border-t border-slate-200">
                Retrieved, not used ({dropped.length})
              </p>
              {dropped.map(c => <ChunkCard key={c.id} chunk={c} muted />)}
            </>
          )}
        </div>
      )}
    </aside>
  );
}

function ChunkCard({ chunk, muted }) {
  return (
    <div className={`rounded-md border border-slate-200 bg-white p-2 ${muted ? 'opacity-60' : ''}`}>
      <div className="flex items-center gap-2 mb-1">
        <span className="text-[10px] uppercase tracking-wide px-1.5 py-0.5 rounded bg-slate-900 text-white">
          {chunk.team}
        </span>
        <span className="text-[11px] text-slate-500 tabular-nums">{chunk.score?.toFixed(3)}</span>
      </div>
      <a href={chunk.url} target="_blank" rel="noreferrer"
         className="text-xs font-medium text-slate-800 hover:underline block truncate">
        {chunk.pageTitle}
      </a>
      {chunk.sectionPath && (
        <div className="text-[11px] text-slate-500 truncate">{chunk.sectionPath}</div>
      )}
      <p className="text-[11px] text-slate-600 mt-1 line-clamp-6 whitespace-pre-wrap">{chunk.text}</p>
    </div>
  );
}
```

- [ ] **Step 5: Wire the panel and the suggestion into `App.jsx`**

Add `const [retrieval, setRetrieval] = useState([]);`, `const [panelOpen, setPanelOpen] = useState(true);` and `const [suggestions, setSuggestions] = useState([]);`. In the `streamChat` loop, handle the new event types:

```jsx
        if (type === 'retrieval') { setRetrieval(data); continue; }
        if (type === 'suggestion') { setSuggestions(data); continue; }
```

Clear both at the start of each send. Render `<ContextPanel chunks={retrieval} open={panelOpen} onToggle={() => setPanelOpen(o => !o)} />` to the right of the thread column, and render suggestions above the composer as buttons that call `setTeam(s.team)`:

```jsx
      {suggestions.map(s => (
        <button key={s.team} onClick={() => setTeam(s.team)}
          className="text-xs px-2 py-1 rounded border border-amber-300 bg-amber-50 text-amber-900">
          No results here — {s.matches} match{s.matches === 1 ? '' : 'es'} in {s.team}. Switch?
        </button>
      ))}
```

- [ ] **Step 6: Verify by hand**

```bash
make dev-up && KA_FIXTURES_DIR=fixtures make run-api &
make run-ui
```

Open `http://localhost:5173` and confirm:
1. The dropdown lists Coupa, Star and HR (the `dev@example.com` default belongs to all three).
2. Asking a Coupa question shows a context panel whose chunks all carry the `coupa` badge, with scores, and a "Retrieved, not used" section.
3. Every chunk shown as "Passed to the model" is one the answer could plausibly have used — spot-check one citation against the panel.
4. Switching to Star starts a new chat and the sidebar no longer shows the Coupa chats.
5. Asking Star "what is the escalation process" surfaces the HR suggestion button, and clicking it switches teams.

- [ ] **Step 7: Update the README**

Add a "Teams" section documenting the three teams, the `X-Team` header, the `X-Dev-User` demo membership table, and — stated plainly — that team is derived from the SharePoint site and **item-level SharePoint permissions are not mirrored**, so a document restricted to a subset of a team is visible to that whole team.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "Add the team selector and retrieval context panel

The panel shows exactly the chunks passed to the model, with the ones
that were retrieved and cut listed separately, plus score and team badge."
```

---

## Not in this plan

Deferred to the SharePoint ingestion plan: the Graph client, `Sites.Selected` app registration, docx/pdf extraction, S3 staging, delta sync, and per-team ingest credentials. The `team.Info.SiteID` field exists for it but is unused here.

Also out of scope, per the spec: real reranking (`RerankTopN` truncates rather than reranks — the context panel will make this visible), cross-team search, and mirroring SharePoint item-level ACLs.
