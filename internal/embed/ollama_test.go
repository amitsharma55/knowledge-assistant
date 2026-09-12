package embed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOllamaEmbedSendsModelAndInput(t *testing.T) {
	var gotPath, gotModel, gotInput string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("request body is not the expected JSON: %v", err)
		}
		gotModel, gotInput = req.Model, req.Input
		_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2,0.3]]}`))
	}))
	defer srv.Close()

	o := Ollama{BaseURL: srv.URL, Model: "nomic-embed-text", Dim: 3, HTTP: srv.Client()}
	v, err := o.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotPath != "/api/embed" {
		t.Errorf("path = %q, want /api/embed", gotPath)
	}
	if gotModel != "nomic-embed-text" {
		t.Errorf("model = %q, want nomic-embed-text", gotModel)
	}
	if gotInput != "hello" {
		t.Errorf("input = %q, want hello", gotInput)
	}
	if len(v) != 3 || v[0] != 0.1 {
		t.Errorf("embedding = %v, want [0.1 0.2 0.3]", v)
	}
}

func TestOllamaEmbedErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"model 'nope' not found"}`))
	}))
	defer srv.Close()

	o := Ollama{BaseURL: srv.URL, Model: "nope", Dim: 768, HTTP: srv.Client()}
	_, err := o.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("want error on 404, got nil")
	}
	// The operator's most likely mistake is an unpulled model, so the
	// server's own message must survive into the error.
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q does not carry the server's message", err)
	}
}

// A wrong KA_EMBED_MODEL yields vectors of the wrong width. OpenSearch would
// reject them at index time with an error that says nothing about the model,
// so the embedder catches it at the source.
func TestOllamaEmbedErrorsOnDimensionMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2,0.3]]}`))
	}))
	defer srv.Close()

	o := Ollama{BaseURL: srv.URL, Model: "wrong-model", Dim: 768, HTTP: srv.Client()}
	_, err := o.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("want error when the vector width is not the configured dimension, got nil")
	}
	for _, want := range []string{"768", "3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s; both widths are needed to diagnose it", err, want)
		}
	}
}

func TestOllamaEmbedErrorsOnEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"embeddings":[]}`))
	}))
	defer srv.Close()

	o := Ollama{BaseURL: srv.URL, Model: "nomic-embed-text", Dim: 768, HTTP: srv.Client()}
	if _, err := o.Embed(context.Background(), "hello"); err == nil {
		t.Fatal("want error on empty embeddings array, got nil")
	}
}
