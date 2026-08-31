package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/services/chat-api/internal/middleware"
	"github.com/example/knowledge-assistant/services/chat-api/internal/repo"
	"github.com/example/knowledge-assistant/services/chat-api/internal/session"
)

type ChatHandler struct {
	Orchestrator *rag.Orchestrator
	Sessions     *session.Store
	Repo         *repo.Repo // if nil, chats are not persisted
	Log          *slog.Logger
}

type chatReq struct {
	SessionID string `json:"sessionId"` // for uploaded-doc scoping
	ChatID    string `json:"chatId"`    // persistent chat; created on first message if empty
	Message   string `json:"message"`
}

// ServeHTTP streams SSE. Frames:
//
//	event: chat\ndata: {"chatId":"...", "title":"..."}\n\n   (once, if a new chat was created)
//	event: citation\ndata: [...]\n\n
//	event: token\ndata: "..."\n\n
//	event: done\ndata: null\n\n
func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req chatReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	scope, ok := middleware.ScopeFromContext(r.Context())
	if !ok {
		http.Error(w, "no team scope", http.StatusForbidden)
		return
	}
	uid := middleware.UserFromContext(r.Context())

	// Ensure a persistent chat exists. Auto-title from first message.
	var chat repo.Chat
	if h.Repo != nil {
		if req.ChatID == "" {
			c, err := h.Repo.CreateChat(r.Context(), uid, scope.Team().Slug(), autoTitle(req.Message))
			if err != nil {
				http.Error(w, "create chat: "+err.Error(), http.StatusInternalServerError)
				return
			}
			chat = c
			payload, _ := json.Marshal(map[string]string{"chatId": chat.ID, "title": chat.Title})
			fmt.Fprintf(w, "event: chat\ndata: %s\n\n", payload)
			flusher.Flush()
		} else {
			ok, err := h.Repo.ChatBelongsTo(r.Context(), uid, scope.Team().Slug(), req.ChatID)
			if err != nil {
				http.Error(w, "lookup chat: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if !ok {
				// Matches the cross-team behaviour elsewhere: don't reveal
				// whether the chat exists, just refuse it.
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			chat.ID = req.ChatID
		}
		if _, err := h.Repo.AppendMessage(r.Context(), chat.ID, "user", req.Message, nil); err != nil {
			h.Log.Warn("persist user message failed", "err", err)
		}
	}

	var sessRetriever rag.Retriever
	if h.Sessions != nil && req.SessionID != "" {
		sessRetriever = h.Sessions.Retriever(uid, req.SessionID, scope.Team().Slug())
	}

	events := make(chan rag.StreamEvent, 32)
	errCh := make(chan error, 1)
	go func() {
		errCh <- h.Orchestrator.Answer(r.Context(), req.Message, scope, sessRetriever, events)
		close(events)
	}()

	var assistantText strings.Builder
	var citations []rag.Chunk
	for ev := range events {
		payload, _ := json.Marshal(ev.Data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload)
		flusher.Flush()
		switch ev.Type {
		case "token":
			if s, ok := ev.Data.(string); ok {
				assistantText.WriteString(s)
			}
		case "citation":
			if cs, ok := ev.Data.([]rag.Chunk); ok {
				citations = cs
			}
		}
	}
	if err := <-errCh; err != nil {
		h.Log.Error("answer failed", "err", err, "chat", chat.ID)
		fmt.Fprintf(w, "event: error\ndata: %q\n\n", err.Error())
		flusher.Flush()
		return
	}
	if h.Repo != nil && assistantText.Len() > 0 {
		if _, err := h.Repo.AppendMessage(r.Context(), chat.ID, "assistant", assistantText.String(), citations); err != nil {
			h.Log.Warn("persist assistant message failed", "err", err)
		}
	}
}

func autoTitle(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) > 60 {
		return msg[:57] + "…"
	}
	return msg
}
