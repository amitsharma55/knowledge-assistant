package main

import "testing"

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
