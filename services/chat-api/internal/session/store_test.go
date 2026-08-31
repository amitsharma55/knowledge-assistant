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

	if err := s.Add(ctx, "user-1", "sess-1", "star", Upload{ID: "u1", Filename: "draft.md"}, chunks); err != nil {
		t.Fatalf("Add: %v", err)
	}

	r := s.Retriever("user-1", "sess-1", "star")
	if r == nil {
		t.Fatal("Retriever for the uploading team is nil, want the session store")
	}
	if other := s.Retriever("user-1", "sess-1", "hr"); other != nil {
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

// TestUploadsAreInvisibleToOtherUsers pins the fix for a hole where sessionID
// alone (same team) was the whole access predicate: a caller in the same
// team who learns or guesses another user's sessionID could retrieve that
// user's uploaded document text. The key must include the uploading user's
// ID, not just sessionID + team.
func TestUploadsAreInvisibleToOtherUsers(t *testing.T) {
	s := NewStore(embed.Mock{Dim: 1024}, time.Minute)
	ctx := context.Background()
	chunks := []rag.Chunk{{ID: "u1", Team: "star", PageTitle: "Draft", Text: "star only upload"}}

	if err := s.Add(ctx, "user-1", "shared-session", "star", Upload{ID: "u1", Filename: "draft.md"}, chunks); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if r := s.Retriever("user-1", "shared-session", "star"); r == nil {
		t.Fatal("Retriever for the uploading user is nil, want the session store")
	}
	if r := s.Retriever("user-2", "shared-session", "star"); r != nil {
		t.Error("an upload made by user-1 is retrievable by user-2 with the same sessionID and team")
	}
}
