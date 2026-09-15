package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestParseFrontMatter(t *testing.T) {
	raw := "---\nurl: https://sf-demo.service-now.com/kb?id=1\nupdated: 2026-08-30\n---\n# AVR Runbook\n\nbody\n"
	meta, body := parseFrontMatter(raw)
	if meta["url"] != "https://sf-demo.service-now.com/kb?id=1" {
		t.Fatalf("url = %q", meta["url"])
	}
	if meta["updated"] != "2026-08-30" {
		t.Fatalf("updated = %q", meta["updated"])
	}
	if body != "# AVR Runbook\n\nbody\n" {
		t.Fatalf("body not stripped to H1: %q", body)
	}
}

func TestParseFrontMatterAbsent(t *testing.T) {
	raw := "# No Front Matter\n\nbody\n"
	meta, body := parseFrontMatter(raw)
	if meta != nil {
		t.Fatalf("meta = %v, want nil", meta)
	}
	if body != raw {
		t.Fatalf("body altered when no front matter present")
	}
}

func TestParseFrontMatterUnterminated(t *testing.T) {
	// A doc that opens with --- but never closes it is treated as having no
	// front matter, so a stray leading rule is never swallowed.
	raw := "---\nnot really front matter\n# Title\n"
	meta, body := parseFrontMatter(raw)
	if meta != nil || body != raw {
		t.Fatalf("unterminated block should be a no-op: meta=%v", meta)
	}
}

func TestLoadFixturesReadsFrontMatter(t *testing.T) {
	dir := t.TempDir()
	doc := "---\nurl: https://sf-demo.coupahost.com/doc/42\nupdated: 2026-08-30\n---\n# AVR Runbook\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "avr-runbook.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	pages, err := loadFixtures(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 {
		t.Fatalf("got %d pages", len(pages))
	}
	p := pages[0]
	if p.WebURL != "https://sf-demo.coupahost.com/doc/42" {
		t.Fatalf("WebURL = %q", p.WebURL)
	}
	if p.Title != "AVR Runbook" {
		t.Fatalf("Title = %q, want the H1", p.Title)
	}
	if strings.Contains(p.Markdown, "url:") {
		t.Fatalf("front matter leaked into body: %q", p.Markdown)
	}
	if !p.UpdatedAt.Equal(time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("UpdatedAt = %v", p.UpdatedAt)
	}
}

func TestS3PageReadsFrontMatter(t *testing.T) {
	mod := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	text := "---\nurl: https://sf-demo.icertis.com/docs/x\nupdated: 2026-08-30\n---\n# Icertis Field Mapping\n\nbody\n"
	p := s3Page("ka-docs", "coupa/", s3src.Doc{
		Key:          "coupa/icertis-field-mapping.md",
		Text:         text,
		LastModified: mod,
	})
	// Front-matter url and date win over the s3:// fallback and object mtime.
	if p.WebURL != "https://sf-demo.icertis.com/docs/x" {
		t.Fatalf("WebURL = %q, want the front-matter url", p.WebURL)
	}
	if !p.UpdatedAt.Equal(time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("UpdatedAt = %v, want the front-matter date", p.UpdatedAt)
	}
	if p.Title != "Icertis Field Mapping" {
		t.Fatalf("Title = %q, want the H1", p.Title)
	}
	if strings.Contains(p.Markdown, "url:") {
		t.Fatalf("front matter leaked into body: %q", p.Markdown)
	}
}
