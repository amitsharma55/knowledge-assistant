// Package repo is the persistence layer for chats and messages.
package repo

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

type Chat struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Message struct {
	ID        string      `json:"id"`
	ChatID    string      `json:"chatId"`
	Role      string      `json:"role"`
	Content   string      `json:"content"`
	Citations []rag.Chunk `json:"citations"`
	CreatedAt time.Time   `json:"createdAt"`
}

type Repo struct{ Pool *pgxpool.Pool }

func New(ctx context.Context, dsn string) (*Repo, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Repo{Pool: pool}, nil
}

func (r *Repo) Close() { r.Pool.Close() }

// CreateChat inserts a fresh chat and returns it.
func (r *Repo) CreateChat(ctx context.Context, userID, title string) (Chat, error) {
	if title == "" {
		title = "New chat"
	}
	var c Chat
	err := r.Pool.QueryRow(ctx,
		`INSERT INTO chats (user_id, title) VALUES ($1, $2)
		 RETURNING id, user_id, title, created_at, updated_at`,
		userID, title,
	).Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// ListChats returns the user's chats updated in the last `days` days,
// most recently updated first.
func (r *Repo) ListChats(ctx context.Context, userID string, days int) ([]Chat, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id, user_id, title, created_at, updated_at
		 FROM chats
		 WHERE user_id = $1 AND updated_at >= NOW() - ($2 || ' days')::interval
		 ORDER BY updated_at DESC`,
		userID, fmt.Sprintf("%d", days),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		var c Chat
		if err := rows.Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateTitle renames a chat.
func (r *Repo) UpdateTitle(ctx context.Context, userID, chatID, title string) error {
	ct, err := r.Pool.Exec(ctx,
		`UPDATE chats SET title = $3, updated_at = NOW()
		 WHERE id = $2 AND user_id = $1`,
		userID, chatID, title)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("not found")
	}
	return nil
}

// DeleteChat removes a chat and cascades its messages.
func (r *Repo) DeleteChat(ctx context.Context, userID, chatID string) error {
	_, err := r.Pool.Exec(ctx,
		`DELETE FROM chats WHERE id = $2 AND user_id = $1`, userID, chatID)
	return err
}

// AppendMessage writes a message and bumps the chat's updated_at.
func (r *Repo) AppendMessage(ctx context.Context, chatID, role, content string, citations []rag.Chunk) (Message, error) {
	cjson, _ := json.Marshal(citations)
	var m Message
	err := r.Pool.QueryRow(ctx,
		`INSERT INTO messages (chat_id, role, content, citations)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, chat_id, role, content, citations, created_at`,
		chatID, role, content, cjson,
	).Scan(&m.ID, &m.ChatID, &m.Role, &m.Content, &cjson, &m.CreatedAt)
	if err != nil {
		return m, err
	}
	_ = json.Unmarshal(cjson, &m.Citations)
	_, _ = r.Pool.Exec(ctx, `UPDATE chats SET updated_at = NOW() WHERE id = $1`, chatID)
	return m, nil
}

// ListMessages returns messages for a chat in chronological order.
// Ownership is verified via user_id.
func (r *Repo) ListMessages(ctx context.Context, userID, chatID string) ([]Message, error) {
	var owner string
	if err := r.Pool.QueryRow(ctx,
		`SELECT user_id FROM chats WHERE id = $1`, chatID).Scan(&owner); err != nil {
		return nil, fmt.Errorf("chat not found")
	}
	if owner != userID {
		return nil, fmt.Errorf("forbidden")
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT id, chat_id, role, content, citations, created_at
		 FROM messages WHERE chat_id = $1 ORDER BY created_at ASC`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var cjson []byte
		if err := rows.Scan(&m.ID, &m.ChatID, &m.Role, &m.Content, &cjson, &m.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(cjson, &m.Citations)
		out = append(out, m)
	}
	return out, rows.Err()
}
