package handler

import (
	"encoding/json"
	"net/http"

	"github.com/example/knowledge-assistant/internal/team"
	"github.com/example/knowledge-assistant/services/chat-api/internal/middleware"
)

// TeamsHandler serves the teams the caller may select. The UI renders its
// dropdown from this, so the team list is never hardcoded in the frontend.
type TeamsHandler struct {
	Registry *team.Registry
}

type teamDTO struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
}

func (h *TeamsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	allowed, _ := middleware.AllowedFromContext(r.Context())
	out := make([]teamDTO, 0, len(allowed))
	for _, t := range allowed {
		info := h.Registry.Info(t)
		out = append(out, teamDTO{Slug: info.Slug, DisplayName: info.DisplayName})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
