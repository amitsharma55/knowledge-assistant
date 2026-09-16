package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/knowledge-assistant/internal/review"
	"github.com/example/knowledge-assistant/internal/team"
	"github.com/example/knowledge-assistant/services/chat-api/internal/middleware"
	"github.com/go-chi/chi/v5"
)

// adminReq builds a scoped request carrying chi URL param id=<id>.
func adminReq(t *testing.T, method, id string) *http.Request {
	t.Helper()
	reg := team.NewRegistry(team.DefaultInfos())
	coupa, _ := reg.Parse("coupa")
	r := httptest.NewRequest(method, "/v1/admin/pending", nil)
	ctx := middleware.ContextWithScopeForTest(r.Context(), coupa, "dev@example.com")
	if id != "" {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	}
	return r.WithContext(ctx)
}

func TestAdmin_List(t *testing.T) {
	store := review.NewMemoryStore()
	_, _ = store.Enqueue(context.Background(), review.Item{Team: "coupa", Filename: "a.pdf", Text: "hi"})
	_, _ = store.Enqueue(context.Background(), review.Item{Team: "star", Filename: "secret.pdf", Text: "hi"})
	h := &AdminHandler{Review: store, Log: slog.Default()}

	rec := httptest.NewRecorder()
	h.List(rec, adminReq(t, http.MethodGet, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("list code = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "a.pdf") {
		t.Fatalf("list missing coupa item: %s", body)
	}
	if strings.Contains(body, "secret.pdf") {
		t.Fatalf("list leaked another team's item: %s", body)
	}
}

func TestAdmin_Reject(t *testing.T) {
	store := review.NewMemoryStore()
	id, _ := store.Enqueue(context.Background(), review.Item{Team: "coupa", Filename: "a.pdf"})
	h := &AdminHandler{Review: store, Log: slog.Default()}

	rec := httptest.NewRecorder()
	h.Reject(rec, adminReq(t, http.MethodPost, id))
	if rec.Code != http.StatusOK {
		t.Fatalf("reject code = %d", rec.Code)
	}
	got, _ := store.Get(context.Background(), id)
	if got.Status != review.Rejected {
		t.Fatalf("status = %q, want rejected", got.Status)
	}
}

func TestAdmin_Reject_NotFound(t *testing.T) {
	h := &AdminHandler{Review: review.NewMemoryStore(), Log: slog.Default()}
	rec := httptest.NewRecorder()
	h.Reject(rec, adminReq(t, http.MethodPost, "nope"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
}

func TestAdmin_Approve_NoIndexer(t *testing.T) {
	store := review.NewMemoryStore()
	id, _ := store.Enqueue(context.Background(), review.Item{Team: "coupa", Filename: "a.pdf", Text: "hi"})
	h := &AdminHandler{Review: store, Log: slog.Default()} // Indexer nil = fixtures mode

	rec := httptest.NewRecorder()
	h.Approve(rec, adminReq(t, http.MethodPost, id))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
	// item must stay pending so it can be approved once indexing is available
	got, _ := store.Get(context.Background(), id)
	if got.Status != review.Pending {
		t.Fatalf("status = %q, want still pending", got.Status)
	}
}
