package ingest

import "testing"

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
