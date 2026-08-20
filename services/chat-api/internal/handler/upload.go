package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/example/knowledge-assistant/internal/extract"
	"github.com/example/knowledge-assistant/internal/ingest"
	"github.com/example/knowledge-assistant/internal/index"
	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/services/chat-api/internal/session"
	"github.com/google/uuid"
)

const maxUploadSize = 25 << 20 // 25 MB

type UploadHandler struct {
	Sessions *session.Store    // for session-scoped uploads
	Indexer  *index.Indexer    // for persist=true (nil if OpenSearch isn't configured)
	Embedder rag.Embedder      // used for the persistent path
	Log      *slog.Logger
}

type uploadResp struct {
	UploadID  string `json:"uploadId"`
	Filename  string `json:"filename"`
	Bytes     int    `json:"bytes"`
	Chunks    int    `json:"chunks"`
	Persisted bool   `json:"persisted"`
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

	uploadID := uuid.NewString()
	page := ingest.Page{
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
		if err := h.Sessions.Add(r.Context(), sessionID, up, chunks); err != nil {
			http.Error(w, "session store failed", http.StatusInternalServerError)
			return
		}
		resp.Chunks = len(chunks)
	}

	if persist {
		if h.Indexer == nil {
			http.Error(w, "persist requested but no indexer configured", http.StatusServiceUnavailable)
			return
		}
		n, err := ingest.Ingest(r.Context(), page, h.Embedder, h.Indexer)
		if err != nil {
			http.Error(w, fmt.Sprintf("index failed: %v", err), http.StatusBadGateway)
			return
		}
		if resp.Chunks == 0 {
			resp.Chunks = n
		}
		resp.Persisted = true
	}

	h.Log.Info("upload accepted",
		"id", uploadID, "file", hdr.Filename, "bytes", len(data),
		"chunks", resp.Chunks, "session", sessionID != "", "persist", persist)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
