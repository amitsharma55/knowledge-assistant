package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/knowledge-assistant/internal/rag"
)

// reply serves one canned Ollama /api/chat response and records the request.
func reply(t *testing.T, content string, status int) (*httptest.Server, *map[string]any) {
	t.Helper()
	got := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/chat") {
			t.Errorf("called %s, want /api/chat", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		if status >= 300 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"model not found"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"content": content},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestRerankAppliesModelOrder(t *testing.T) {
	srv, got := reply(t, `{"order":[3,1,2]}`, 200)
	o := Ollama{BaseURL: srv.URL, Model: "gpt-oss:20b", HTTP: srv.Client()}

	out, err := o.Rerank(context.Background(), "q", chunks("a", "b", "c"))
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if ids(out) != "cab" {
		t.Errorf("got %q, want %q", ids(out), "cab")
	}

	// The schema must be sent: gpt-oss emits reasoning text first, so an
	// unconstrained reply is unparseable.
	if _, ok := (*got)["format"]; !ok {
		t.Error("request carried no structured-output format")
	}
	opts, _ := (*got)["options"].(map[string]any)
	if opts["temperature"] != float64(0) {
		t.Errorf("temperature = %v, want 0 so ranking is reproducible", opts["temperature"])
	}
	// Set to reduce variance, though measurably not to eliminate it --
	// see the note beside them in ollama.go.
	if _, ok := opts["seed"]; !ok {
		t.Error("no seed sent; the ranking must be reproducible run to run")
	}
}

func TestRerankRestoresChunksTheModelDropped(t *testing.T) {
	srv, _ := reply(t, `{"order":[2]}`, 200)
	o := Ollama{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}

	out, err := o.Rerank(context.Background(), "q", chunks("a", "b", "c"))
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(out) != 3 || ids(out) != "bac" {
		t.Errorf("got %q (%d chunks), want bac (3)", ids(out), len(out))
	}
}

func TestRerankErrorsAreReportedNotSwallowed(t *testing.T) {
	// The orchestrator decides to fall back; the client's job is to say
	// clearly what went wrong.
	srv, _ := reply(t, "", 404)
	o := Ollama{BaseURL: srv.URL, Model: "missing", HTTP: srv.Client()}
	if _, err := o.Rerank(context.Background(), "q", chunks("a", "b")); err == nil {
		t.Fatal("want an error for a 404")
	} else if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error %q should carry the server's explanation", err)
	}

	srv2, _ := reply(t, "We need to rank the chunks...", 200)
	o2 := Ollama{BaseURL: srv2.URL, Model: "m", HTTP: srv2.Client()}
	if _, err := o2.Rerank(context.Background(), "q", chunks("a", "b")); err == nil {
		t.Error("want an error when the reply is not JSON")
	}
}

func TestRerankSkipsTheCallWhenThereIsNothingToOrder(t *testing.T) {
	srv, got := reply(t, `{"order":[1]}`, 200)
	o := Ollama{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	one := chunks("a")
	out, err := o.Rerank(context.Background(), "q", one)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(out) != 1 {
		t.Errorf("got %d chunks, want 1", len(out))
	}
	if len(*got) != 0 {
		t.Error("a single chunk should not reach the model at all")
	}
}

var _ rag.Reranker = Ollama{}
