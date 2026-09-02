// Package ingest is the shared chunk-and-index pipeline used by both the
// CronJob-style CLI ingester and the interactive "Save to knowledge base"
// upload path in chat-api. Keeps behavior identical across entry points.
package ingest

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"time"

	"github.com/example/knowledge-assistant/internal/chunker"
	"github.com/example/knowledge-assistant/internal/index"
	"github.com/example/knowledge-assistant/internal/rag"
)

type Page struct {
	Team      string
	SpaceKey  string
	PageID    string
	Title     string
	URL       string
	Markdown  string
	UpdatedAt time.Time
	ACLGroups []string
}

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// Page chunks + embeds + bulk-indexes a single page. Returns the number of
// chunks written.
func Ingest(ctx context.Context, p Page, e Embedder, idx *index.Indexer) (int, error) {
	var docs []index.Doc
	for _, c := range chunker.Split(p.Markdown, 800, 100) {
		vec, err := e.Embed(ctx, chunker.EmbedText(p.Title, c.SectionPath, c.Text))
		if err != nil {
			return 0, err
		}
		docs = append(docs, index.Doc{
			ID:          docID(p.Team, p.PageID, c.SectionPath, c.Text),
			Team:        p.Team,
			SpaceKey:    p.SpaceKey,
			PageID:      p.PageID,
			PageTitle:   p.Title,
			SectionPath: c.SectionPath,
			URL:         p.URL,
			Text:        c.Text,
			Embedding:   vec,
			UpdatedAt:   p.UpdatedAt.Format(time.RFC3339),
			ACLGroups:   p.ACLGroups,
		})
	}
	if err := idx.Bulk(ctx, docs); err != nil {
		return 0, err
	}
	return len(docs), nil
}

// Chunks converts markdown into rag.Chunk records suitable for a MemoryStore
// (session-scoped retrieval, no OpenSearch round-trip). No embedding yet —
// the caller's store embeds on Upsert.
func Chunks(p Page) []rag.Chunk {
	var out []rag.Chunk
	for i, c := range chunker.Split(p.Markdown, 800, 100) {
		out = append(out, rag.Chunk{
			ID:          p.Team + ":" + p.PageID + ":" + itoa(i),
			Team:        p.Team,
			SpaceKey:    p.SpaceKey,
			PageID:      p.PageID,
			PageTitle:   p.Title,
			SectionPath: c.SectionPath,
			URL:         p.URL,
			Text:        c.Text,
			ACLGroups:   p.ACLGroups,
		})
	}
	return out
}

// docID scopes a document's identity to its team, so byte-identical content
// (shared boilerplate, a copied policy section) in two different teams
// produces two different ids rather than colliding and overwriting each
// other in the shared OpenSearch index.
func docID(team, pageID, section, text string) string {
	h := sha1.New()
	h.Write([]byte(team))
	h.Write([]byte{0})
	h.Write([]byte(pageID))
	h.Write([]byte{0})
	h.Write([]byte(section))
	h.Write([]byte{0})
	h.Write([]byte(text))
	return team + ":" + pageID + ":" + hex.EncodeToString(h.Sum(nil))[:12]
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
