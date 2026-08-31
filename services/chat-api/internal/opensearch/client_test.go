package opensearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestScopeFilterZeroGroupsStillConstrainsACL pins the fix for a divergence
// from MemoryStore.aclOK: a caller with zero groups must still get an
// aclGroups constraint (limiting them to unrestricted chunks), not an absent
// ACL clause that would expose every doc in the team.
func TestScopeFilterZeroGroupsStillConstrainsACL(t *testing.T) {
	scope := scopeFor(t, "coupa", "coupa") // scopeFor always yields zero groups
	if len(scope.Groups()) != 0 {
		t.Fatalf("test setup: expected zero groups, got %v", scope.Groups())
	}

	filter := scopeFilter(scope)
	raw, err := json.Marshal(filter)
	if err != nil {
		t.Fatalf("marshal filter: %v", err)
	}
	if !strings.Contains(string(raw), `"aclGroups"`) {
		t.Fatalf("zero-group scope produced a filter with no aclGroups constraint at all, so every doc in the team (including ACL-restricted ones) is visible: %s", raw)
	}
}

// containsTermTeam walks the filter looking for {"term":{"team":<slug>}} that
// sits inside a bool.must array — i.e. actually constrains the query, not
// merely present somewhere non-binding like a "should" clause.
func containsTermTeam(v any, slug string) bool {
	return findBindingTermTeam(v, slug, true)
}

// findBindingTermTeam walks the filter tree. binding tracks whether the
// current position is reachable only through must-context (AND) clauses;
// should/must_not clauses are non-binding, since a term there does not
// constrain the query on its own.
func findBindingTermTeam(v any, slug string, binding bool) bool {
	switch n := v.(type) {
	case map[string]any:
		if term, ok := n["term"].(map[string]any); ok {
			if got, ok := term["team"].(string); ok && got == slug {
				return binding
			}
		}
		if b, ok := n["bool"].(map[string]any); ok {
			if must, ok := b["must"]; ok && findBindingTermTeam(must, slug, true) {
				return true
			}
			if should, ok := b["should"]; ok && findBindingTermTeam(should, slug, false) {
				return true
			}
			if mustNot, ok := b["must_not"]; ok && findBindingTermTeam(mustNot, slug, false) {
				return true
			}
			return false
		}
		for _, child := range n {
			if findBindingTermTeam(child, slug, binding) {
				return true
			}
		}
	case []any:
		for _, child := range n {
			if findBindingTermTeam(child, slug, binding) {
				return true
			}
		}
	}
	return false
}
