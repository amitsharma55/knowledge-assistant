package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/example/knowledge-assistant/services/chat-api/internal/middleware"
	"github.com/example/knowledge-assistant/services/chat-api/internal/repo"
	"github.com/go-chi/chi/v5"
)

type ChatsHandler struct {
	Repo *repo.Repo
	Log  *slog.Logger
}

func (h *ChatsHandler) List(w http.ResponseWriter, r *http.Request) {
	chats, err := h.Repo.ListChats(r.Context(), middleware.UserFromContext(r.Context()), 30)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if chats == nil {
		chats = []repo.Chat{}
	}
	writeJSON(w, chats)
}

func (h *ChatsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	c, err := h.Repo.CreateChat(r.Context(), middleware.UserFromContext(r.Context()), body.Title)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, c)
}

func (h *ChatsHandler) Messages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	msgs, err := h.Repo.ListMessages(r.Context(), middleware.UserFromContext(r.Context()), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if msgs == nil {
		msgs = []repo.Message{}
	}
	writeJSON(w, msgs)
}

func (h *ChatsHandler) Rename(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		http.Error(w, "title required", http.StatusBadRequest)
		return
	}
	if err := h.Repo.UpdateTitle(r.Context(), middleware.UserFromContext(r.Context()), id, body.Title); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ChatsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Repo.DeleteChat(r.Context(), middleware.UserFromContext(r.Context()), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
