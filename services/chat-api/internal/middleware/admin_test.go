package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/knowledge-assistant/internal/team"
)

func adminReq(user string) *http.Request {
	reg := team.NewRegistry(team.DefaultInfos())
	coupa, _ := reg.Parse("coupa")
	r := httptest.NewRequest(http.MethodGet, "/v1/admin/pending", nil)
	return r.WithContext(ContextWithScopeForTest(context.Background(), coupa, user))
}

func TestRequireAdmin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mw := RequireAdmin([]string{"dev@example.com"})

	t.Run("allows admin", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mw(next).ServeHTTP(rec, adminReq("dev@example.com"))
		if rec.Code != http.StatusOK {
			t.Fatalf("admin blocked: %d", rec.Code)
		}
	})
	t.Run("blocks non-admin", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mw(next).ServeHTTP(rec, adminReq("someone@example.com"))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("non-admin allowed: %d", rec.Code)
		}
	})
}
