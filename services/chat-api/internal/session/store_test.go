package session

import (
	"context"
	"testing"
	"time"

	"github.com/example/knowledge-assistant/internal/embed"
	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/team"
)

func TestUploadsAreInvisibleUnderAnotherTeam(t *testing.T) {
	s := NewStore(embed.Mock{Dim: 1024}, time.Minute)
	ctx := context.Background()
	chunks := []rag.Chunk{{ID: "u1", Team: "star", PageTitle: "Draft", Text: "star only upload"}}

	if err := s.Add(ctx, "sess-1", "star", Upload{ID: "u1", Filename: "draft.md"}, chunks); err != nil {
		t.Fatalf("Add: %v", err)
	}

	r := s.Retriever("sess-1", "star")
	if r == nil {
		t.Fatal("Retriever for the uploading team is nil, want the session store")
	}
	if other := s.Retriever("sess-1", "hr"); other != nil {
		t.Error("an upload made under star is retrievable under hr")
	}

	// A non-nil retriever that returns zero results would still be broken:
	// this is the regression test for the Task 2 bug where session-uploaded
	// chunks carried Team == "" and were silently dropped by the team
	// filter in MemoryStore.Search. Prove the uploaded chunk actually comes
	// back under its own team.
	reg := team.NewRegistry(team.DefaultInfos())
	star, err := reg.Parse("star")
	if err != nil {
		t.Fatalf("parse star: %v", err)
	}
	scope := rag.NewScope(star, []team.Team{star}, nil)

	got, err := r.Search(ctx, "star only upload", scope, 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Search returned no chunks for the uploading team; the uploaded chunk was dropped")
	}
	found := false
	for _, c := range got {
		if c.ID == "u1" {
			found = true
		}
	}
	if !found {
		t.Errorf("Search results %v do not include the uploaded chunk u1", got)
	}
}
