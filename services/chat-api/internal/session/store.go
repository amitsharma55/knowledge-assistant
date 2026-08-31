// Package session holds per-session ephemeral state — right now just
// uploaded documents that should participate in retrieval only within the
// scope of the chat session that uploaded them.
package session

import (
	"context"
	"sync"
	"time"

	"github.com/example/knowledge-assistant/services/chat-api/internal/opensearch"
	"github.com/example/knowledge-assistant/internal/rag"
)

type entry struct {
	store    *opensearch.MemoryStore
	uploads  []Upload
	lastUsed time.Time
}

type Upload struct {
	ID       string
	Filename string
	Bytes    int
	Chunks   int
}

type Store struct {
	mu       sync.Mutex
	sessions map[string]*entry
	embedder rag.Embedder
	ttl      time.Duration
}

func NewStore(e rag.Embedder, ttl time.Duration) *Store {
	s := &Store{sessions: map[string]*entry{}, embedder: e, ttl: ttl}
	go s.gc()
	return s
}

// key scopes a session's uploads to the team they were made under, so an
// upload cannot surface in a different team's retrieval.
func key(sessionID, teamSlug string) string { return sessionID + "\x00" + teamSlug }

// Add creates or updates the session's memory retriever with these chunks.
func (s *Store) Add(ctx context.Context, sessionID, teamSlug string, u Upload, chunks []rag.Chunk) error {
	s.mu.Lock()
	k := key(sessionID, teamSlug)
	e, ok := s.sessions[k]
	if !ok {
		e = &entry{store: opensearch.NewMemoryStore(s.embedder)}
		s.sessions[k] = e
	}
	e.uploads = append(e.uploads, u)
	e.lastUsed = time.Now()
	s.mu.Unlock()
	for _, c := range chunks {
		if err := e.store.Upsert(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

// Retriever returns the session's retriever for this team, or nil if the
// session has no uploads under that team.
func (s *Store) Retriever(sessionID, teamSlug string) rag.Retriever {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.sessions[key(sessionID, teamSlug)]
	if !ok {
		return nil
	}
	e.lastUsed = time.Now()
	return e.store
}

func (s *Store) gc() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-s.ttl)
		s.mu.Lock()
		for id, e := range s.sessions {
			if e.lastUsed.Before(cutoff) {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}
