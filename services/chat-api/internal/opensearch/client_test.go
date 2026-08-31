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
