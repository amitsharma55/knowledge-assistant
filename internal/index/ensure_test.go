package index

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mappingServer serves a _mapping response describing an existing index with
// the given vector dimension, and records any index-creation attempt.
func mappingServer(t *testing.T, dim int, created *bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			*created = true
			w.WriteHeader(http.StatusOK)
			return
		}
		fmt.Fprintf(w, `{"kb-chunks":{"mappings":{"properties":{
			"team":{"type":"keyword"},
			"embedding":{"type":"knn_vector","dimension":%d}
		}}}}`, dim)
	}))
}

func TestEnsureIndexAcceptsMatchingDimension(t *testing.T) {
	created := false
	srv := mappingServer(t, 768, &created)
	defer srv.Close()

	i := &Indexer{BaseURL: srv.URL, Index: "kb-chunks", HTTP: srv.Client()}
	if err := i.EnsureIndex(context.Background(), 768); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if created {
		t.Error("EnsureIndex recreated an index that already matched")
	}
}

// Switching embedding models changes the vector width. The old index still
// has the team field, so the existing check passes it; without a dimension
// check the mismatch surfaces later as an opaque bulk-write failure that
// names neither the model nor the config that chose it.
func TestEnsureIndexRejectsDimensionMismatch(t *testing.T) {
	created := false
	srv := mappingServer(t, 1024, &created)
	defer srv.Close()

	i := &Indexer{BaseURL: srv.URL, Index: "kb-chunks", HTTP: srv.Client()}
	err := i.EnsureIndex(context.Background(), 768)
	if err == nil {
		t.Fatal("EnsureIndex accepted a 1024-dimension index for a 768-dimension embedder")
	}
	for _, want := range []string{"1024", "768"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s; both widths are needed to diagnose it", err, want)
		}
	}
	if !strings.Contains(err.Error(), "reindex") {
		t.Errorf("error %q does not tell the operator to reindex", err)
	}
	if created {
		t.Error("EnsureIndex recreated the index; that would destroy the existing corpus silently")
	}
}
