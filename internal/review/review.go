// Package review holds documents uploaded for the knowledge base between the
// PII scan and admin approval. Nothing here is indexed; promotion to the index
// happens in the admin handler on approval. The Store interface is the seam:
// MemoryStore is the demo backend, and an S3-backed durable store drops in for
// production without changing callers.
package review

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/example/knowledge-assistant/internal/pii"
	"github.com/google/uuid"
)

type Status string

const (
	Pending  Status = "pending"
	Approved Status = "approved"
	Rejected Status = "rejected"
)

var ErrNotFound = errors.New("review: item not found")

type Item struct {
	ID        string        `json:"id"`
	Team      string        `json:"team"`
	Uploader  string        `json:"uploader"`
	Filename  string        `json:"filename"`
	Text      string        `json:"-"` // extracted text for ingest; not sent to the UI
	Findings  []pii.Finding `json:"findings"`
	Status    Status        `json:"status"`
	Bytes     int           `json:"bytes"`
	CreatedAt time.Time     `json:"createdAt"`
}

type Store interface {
	Enqueue(ctx context.Context, it Item) (string, error)
	List(ctx context.Context, team string) ([]Item, error) // pending only, team-scoped
	Get(ctx context.Context, id string) (Item, error)
	SetStatus(ctx context.Context, id string, s Status) error
}

type memoryStore struct {
	mu    sync.Mutex
	items map[string]Item
}

func NewMemoryStore() Store { return &memoryStore{items: map[string]Item{}} }

func (m *memoryStore) Enqueue(_ context.Context, it Item) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	it.Status = Pending
	if it.CreatedAt.IsZero() {
		it.CreatedAt = time.Now()
	}
	m.items[it.ID] = it
	return it.ID, nil
}

func (m *memoryStore) List(_ context.Context, team string) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Item
	for _, it := range m.items {
		if it.Team == team && it.Status == Pending {
			out = append(out, it)
		}
	}
	return out, nil
}

func (m *memoryStore) Get(_ context.Context, id string) (Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	return it, nil
}

func (m *memoryStore) SetStatus(_ context.Context, id string, s Status) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return ErrNotFound
	}
	it.Status = s
	m.items[id] = it
	return nil
}
