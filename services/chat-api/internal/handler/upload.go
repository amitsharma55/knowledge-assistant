package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/example/knowledge-assistant/internal/extract"
	"github.com/example/knowledge-assistant/internal/ingest"
	"github.com/example/knowledge-assistant/internal/pii"
	"github.com/example/knowledge-assistant/internal/review"
	"github.com/example/knowledge-assistant/services/chat-api/internal/middleware"
	"github.com/example/knowledge-assistant/services/chat-api/internal/session"
	"github.com/google/uuid"
)

const maxUploadSize = 25 << 20 // 25 MB

type UploadHandler struct {
	Sessions *session.Store // session-scoped uploads (unchanged)
	Detector pii.Detector   // scans persist-path uploads before queueing
	Review   review.Store   // pending queue; admin approval (not upload) indexes
	Log      *slog.Logger
}

type uploadResp struct {
	UploadID string        `json:"uploadId"`
	Filename string        `json:"filename"`
	Bytes    int           `json:"bytes"`
	Chunks   int           `json:"chunks,omitempty"`
	Status   string        `json:"status,omitempty"`
	ReviewID string        `json:"reviewId,omitempty"`
	Findings []pii.Finding `json:"findings,omitempty"`
}

func (h *UploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "file too large or malformed multipart", http.StatusBadRequest)
		return
	}
	sessionID := r.FormValue("sessionId")
	persist := r.FormValue("persist") == "true"
	if sessionID == "" && !persist {
		http.Error(w, "sessionId required unless persist=true", http.StatusBadRequest)
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUploadSize+1))
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	if len(data) > maxUploadSize {
		http.Error(w, "file exceeds 25MB", http.StatusRequestEntityTooLarge)
		return
	}

	text, err := extract.FromBytes(hdr.Filename, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		http.Error(w, "no extractable text (scanned PDF? OCR not enabled)", http.StatusUnprocessableEntity)
		return
	}

	scope, ok := middleware.ScopeFromContext(r.Context())
	if !ok {
		http.Error(w, "no team scope", http.StatusForbidden)
		return
	}
	uid := middleware.UserFromContext(r.Context())

	uploadID := uuid.NewString()
	page := ingest.Page{
		Team:      scope.Team().Slug(),
		SpaceKey:  "UPLOAD",
		PageID:    uploadID,
		Title:     hdr.Filename,
		URL:       "upload://" + uploadID + "/" + hdr.Filename,
		Markdown:  text,
		UpdatedAt: time.Now(),
	}

	resp := uploadResp{UploadID: uploadID, Filename: hdr.Filename, Bytes: len(data)}

	if sessionID != "" && h.Sessions != nil {
		chunks := ingest.Chunks(page)
		up := session.Upload{ID: uploadID, Filename: hdr.Filename, Bytes: len(data), Chunks: len(chunks)}
		if err := h.Sessions.Add(r.Context(), uid, sessionID, scope.Team().Slug(), up, chunks); err != nil {
			http.Error(w, "session store failed", http.StatusInternalServerError)
			return
		}
		resp.Chunks = len(chunks)
	}

	if persist {
		// Persisting to the team knowledge base is gated: scan first, hard-block
		// high-severity PII, and otherwise enqueue for human review. The document
		// is NOT indexed here — only admin approval promotes it to the index.
		findings := h.Detector.Scan(text)
		if pii.HasHigh(findings) {
			http.Error(w, "this document appears to contain sensitive information "+
				"(e.g. SSN or card number); remove it and upload again",
				http.StatusUnprocessableEntity)
			return
		}
		reviewID, err := h.Review.Enqueue(r.Context(), review.Item{
			Team:     scope.Team().Slug(),
			Uploader: uid,
			Filename: hdr.Filename,
			Text:     text,
			Findings: findings, // low-severity only reaches here
			Bytes:    len(data),
		})
		if err != nil {
			http.Error(w, "review enqueue failed", http.StatusInternalServerError)
			return
		}
		resp.Status = "pending_review"
		resp.ReviewID = reviewID
		resp.Findings = findings
	}

	h.Log.Info("upload accepted",
		"id", uploadID, "file", hdr.Filename, "bytes", len(data),
		"chunks", resp.Chunks, "session", sessionID != "", "persist", persist,
		"review", resp.ReviewID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
