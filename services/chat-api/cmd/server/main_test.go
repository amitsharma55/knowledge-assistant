package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/knowledge-assistant/internal/embed"
	"github.com/example/knowledge-assistant/services/chat-api/internal/opensearch"
)

// TestPreloadFixturesRejectsUnregisteredTeamDirectory guards against the
// regression where preloadFixtures trusted any subdirectory name under the
// fixtures root as a team, with no validation against team.Registry. A
// stray directory (typo, a macOS ".DS_Store"-as-dir artifact, etc.) must
// never become a phantom team stamped onto chunks and served through
// rag.Scope filtering.
func TestPreloadFixturesRejectsUnregisteredTeamDirectory(t *testing.T) {
	dir := t.TempDir()
	bogus := filepath.Join(dir, "not-a-real-team")
	if err := os.MkdirAll(bogus, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bogus, "page.md"), []byte("# Title\n\nbody text"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	store := opensearch.NewMemoryStore(embed.Mock{Dim: 8})
	total, err := preloadFixtures(context.Background(), store, dir)

	if err == nil {
		t.Fatal("preloadFixtures accepted an unregistered team directory, want an error")
	}
	if total != 0 {
		t.Errorf("preloadFixtures wrote %d chunks from an unregistered team directory, want 0 (no servable chunks)", total)
	}
}
