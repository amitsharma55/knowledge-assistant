package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/knowledge-assistant/internal/index"
	"github.com/example/knowledge-assistant/internal/ingest"
	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/review"
	"github.com/example/knowledge-assistant/services/chat-api/internal/middleware"
	"github.com/go-chi/chi/v5"
)

// AdminHandler serves the human review queue: list pending uploads for the
// caller's team, and approve (index) or reject them. Approval is the only path
// that promotes an uploaded document into the shared index.
type AdminHandler struct {
	Review   review.Store
	Indexer  *index.Indexer // nil in fixtures mode; Approve then returns 503
	Embedder rag.Embedder
	Log      *slog.Logger
}

func (h *AdminHandler) List(w http.ResponseWriter, r *http.Request) {
	scope, ok := middleware.ScopeFromContext(r.Context())
	if !ok {
		http.Error(w, "no team scope", http.StatusForbidden)
		return
	}
	items, err := h.Review.List(r.Context(), scope.Team().Slug())
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	if items == nil {
		items = []review.Item{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func (h *AdminHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Review.SetStatus(r.Context(), id, review.Rejected); err != nil {
		h.status404or500(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Approve promotes the reviewed document into the index, then marks it
// approved. Status advances only after a successful index so a transient index
// failure leaves the item pending and retryable.
func (h *AdminHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	it, err := h.Review.Get(r.Context(), id)
	if err != nil {
		h.status404or500(w, err)
		return
	}
	if h.Indexer == nil {
		http.Error(w, "indexing unavailable (fixtures mode); run with OpenSearch configured", http.StatusServiceUnavailable)
		return
	}
	// Rebuild the ingest.Page exactly as the upload path did, so indexed output
	// is identical to the pre-review behavior — only gated by approval.
	page := ingest.Page{
		Team:      it.Team,
		SpaceKey:  "UPLOAD",
		PageID:    it.ID,
		Title:     it.Filename,
		URL:       "upload://" + it.ID + "/" + it.Filename,
		Markdown:  it.Text,
		UpdatedAt: time.Now(),
	}
	if _, err := ingest.Ingest(r.Context(), page, h.Embedder, h.Indexer); err != nil {
		http.Error(w, "index failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if err := h.Review.SetStatus(r.Context(), id, review.Approved); err != nil {
		http.Error(w, "status update failed", http.StatusInternalServerError)
		return
	}
	h.Log.Info("review approved", "id", it.ID, "team", it.Team, "file", it.Filename)
	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) status404or500(w http.ResponseWriter, err error) {
	if errors.Is(err, review.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.Error(w, "internal error", http.StatusInternalServerError)
}
