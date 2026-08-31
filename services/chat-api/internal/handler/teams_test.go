package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/knowledge-assistant/internal/team"
	"github.com/example/knowledge-assistant/services/chat-api/internal/middleware"
)

func TestTeamsListsOnlyTheCallersTeams(t *testing.T) {
	reg := team.NewRegistry(team.DefaultInfos())
	star, _ := reg.Parse("star")
	hr, _ := reg.Parse("hr")

	h := &TeamsHandler{Registry: reg}
	req := httptest.NewRequest(http.MethodGet, "/v1/teams", nil)
	req = req.WithContext(middleware.ContextWithAllowedForTest(context.Background(), []team.Team{star, hr}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %q)", err, rec.Body.String())
	}
	if len(got) != 2 {
		t.Fatalf("returned %d teams, want 2", len(got))
	}
	for _, g := range got {
		if g.Slug == "coupa" {
			t.Error("listing includes coupa, a team the caller does not belong to")
		}
		if g.DisplayName == "" {
			t.Errorf("team %q has no display name", g.Slug)
		}
	}
}
