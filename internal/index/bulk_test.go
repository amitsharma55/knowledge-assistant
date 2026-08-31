package index

import (
	"context"
	"strings"
	"testing"
)

func TestBulkRejectsDocsWithoutATeam(t *testing.T) {
	i := &Indexer{BaseURL: "http://unused", Index: "kb-chunks"}
	err := i.Bulk(context.Background(), []Doc{
		{ID: "c1", Team: "coupa", Text: "fine"},
		{ID: "c2", Text: "no team"},
	})
	if err == nil {
		t.Fatal("Bulk accepted a doc with no team; unstamped chunks are visible to every team's filter")
	}
	if !strings.Contains(err.Error(), "c2") {
		t.Errorf("error %q does not name the offending doc", err)
	}
}
