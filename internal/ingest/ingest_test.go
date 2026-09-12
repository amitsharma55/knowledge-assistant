package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/knowledge-assistant/internal/index"
)

// TestDocIDScopedByTeam guards against the regression where docID hashed
// only pageID + section + text, so two teams with byte-identical content
// (shared boilerplate, a copied policy section) under the same pageID and
// section would collide on the same OpenSearch document id and overwrite
// each other — the later ingest silently winning and the earlier team
// losing a document it should still have.
func TestDocIDScopedByTeam(t *testing.T) {
	id1 := docID("coupa", "page-1", "Intro", "shared boilerplate text")
	id2 := docID("star", "page-1", "Intro", "shared boilerplate text")
	if id1 == id2 {
		t.Fatalf("docID collided for two different teams with identical content: %q", id1)
	}
}

// captureEmbedder records every string handed to Embed.
type captureEmbedder struct{ seen []string }

func (c *captureEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	c.seen = append(c.seen, text)
	return []float32{0.1, 0.2}, nil
}

// TestIngestEmbedsTitleAndSection pins the fix for chunks being embedded as
// bare section bodies. chunker.Split keeps the heading in SectionPath, so a
// section called "Operations" under "AVR SOAP Service" was embedded without
// the words "AVR" or "SOAP" -- the terms someone actually searches with --
// and ranked far below less relevant chunks. The stored Text stays the clean
// body; only what reaches the embedder changes.
func TestIngestEmbedsTitleAndSection(t *testing.T) {
	e := &captureEmbedder{}
	p := Page{
		Team: "coupa", PageID: "avr-soap-service", Title: "AVR SOAP Service",
		Markdown: "# AVR SOAP Service\n\nIntro text.\n\n## Operations\n\nThe middleware uses four of them.\n",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors":false,"items":[]}`))
	}))
	defer srv.Close()
	idx := &index.Indexer{BaseURL: srv.URL, Index: "kb-chunks", HTTP: srv.Client()}

	if _, err := Ingest(context.Background(), p, e, idx); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	var operations string
	for _, s := range e.seen {
		if strings.Contains(s, "The middleware uses four of them") {
			operations = s
		}
	}
	if operations == "" {
		t.Fatalf("the Operations section was never embedded; saw %q", e.seen)
	}
	for _, want := range []string{"AVR SOAP Service", "Operations"} {
		if !strings.Contains(operations, want) {
			t.Errorf("embedded text for the Operations section = %q, missing %q", operations, want)
		}
	}
}
