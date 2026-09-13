package main

import (
	"testing"
	"time"

	"github.com/example/knowledge-assistant/services/ingestion/internal/s3src"
)

// TestDocIDScopedByTeam guards the same regression as
// internal/ingest.TestDocIDScopedByTeam, but for the indexer CLI's own
// docID (the one actually used by the real GitLab/fixtures ingestion
// path): two teams with byte-identical content under the same pageID and
// section must not collide on the same OpenSearch document id.
func TestDocIDScopedByTeam(t *testing.T) {
	id1 := docID("coupa", "page-1", "Intro", "shared boilerplate text")
	id2 := docID("star", "page-1", "Intro", "shared boilerplate text")
	if id1 == id2 {
		t.Fatalf("docID collided for two different teams with identical content: %q", id1)
	}
}

func TestS3PageMapping(t *testing.T) {
	mod := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	p := s3Page("ka-docs", "coupa/", s3src.Doc{
		Key:          "coupa/avr-field-mapping.md",
		Text:         "# AVR Field Mapping\n\nbody",
		LastModified: mod,
	})
	if p.ID != "avr-field-mapping" {
		t.Fatalf("PageID = %q, want avr-field-mapping", p.ID)
	}
	if p.Title != "AVR Field Mapping" {
		t.Fatalf("Title = %q, want the H1", p.Title)
	}
	if p.WebURL != "s3://ka-docs/coupa/avr-field-mapping.md" {
		t.Fatalf("WebURL = %q", p.WebURL)
	}
	if !p.UpdatedAt.Equal(mod) {
		t.Fatalf("UpdatedAt not carried through")
	}
}
