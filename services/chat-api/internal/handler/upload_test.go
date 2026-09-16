package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/knowledge-assistant/internal/pii"
	"github.com/example/knowledge-assistant/internal/review"
	"github.com/example/knowledge-assistant/internal/team"
	"github.com/example/knowledge-assistant/services/chat-api/internal/middleware"
)

func multipartTxt(t *testing.T, field, filename, body, persist string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile(field, filename)
	_, _ = fw.Write([]byte(body))
	_ = w.WriteField("persist", persist)
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

// scopedUpload builds a persist-mode upload request already carrying the team
// scope + user the middleware would install.
func scopedUpload(t *testing.T, filename, body string) *http.Request {
	t.Helper()
	reg := team.NewRegistry(team.DefaultInfos())
	coupa, err := reg.Parse("coupa")
	if err != nil {
		t.Fatal(err)
	}
	buf, ct := multipartTxt(t, "file", filename, body, "true")
	req := httptest.NewRequest(http.MethodPost, "/v1/uploads", buf)
	req.Header.Set("Content-Type", ct)
	return req.WithContext(middleware.ContextWithScopeForTest(req.Context(), coupa, "dev@example.com"))
}

func TestUpload_Persist_CleanEnqueues(t *testing.T) {
	store := review.NewMemoryStore()
	h := &UploadHandler{Review: store, Detector: pii.NewRegexDetector(), Log: slog.Default()}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, scopedUpload(t, "runbook.txt", "Resend the invoice from Coupa."))

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "pending_review" || resp["reviewId"] == "" {
		t.Fatalf("unexpected response: %v", resp)
	}
	if list, _ := store.List(context.Background(), "coupa"); len(list) != 1 {
		t.Fatalf("expected 1 pending item, got %d", len(list))
	}
}

func TestUpload_Persist_SensitiveBlocks(t *testing.T) {
	store := review.NewMemoryStore()
	h := &UploadHandler{Review: store, Detector: pii.NewRegexDetector(), Log: slog.Default()}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, scopedUpload(t, "hr.txt", "Employee SSN 123-45-6789."))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422; body = %s", rec.Code, rec.Body.String())
	}
	if list, _ := store.List(context.Background(), "coupa"); len(list) != 0 {
		t.Fatalf("sensitive doc must not be enqueued, got %d", len(list))
	}
}
